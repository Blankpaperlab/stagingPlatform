package contracts

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"stagehand/internal/recorder"
)

type GenerateOptions struct {
	AgentName string
}

type GenerateSummary struct {
	AllowedActions    int
	RestrictedActions int
	ForbiddenActions  int
	Models            []string
	UnknownRisk       int
}

type actionAccumulator struct {
	action Action
	models map[string]bool
}

func GenerateFromRun(run recorder.Run, opts GenerateOptions) (File, GenerateSummary) {
	agentName := strings.TrimSpace(opts.AgentName)
	if agentName == "" {
		agentName = run.SessionName
	}

	accumulators := map[string]*actionAccumulator{}
	models := map[string]bool{}
	for _, interaction := range run.Interactions {
		selector, ok := selectorFromInteraction(interaction)
		if !ok {
			continue
		}
		key := selectorKey(selector)
		accumulator := accumulators[key]
		if accumulator == nil {
			accumulator = &actionAccumulator{
				action: Action{
					Service:   selector.Service,
					Operation: selector.Operation,
					Tool:      selector.Tool,
				},
				models: map[string]bool{},
			}
			accumulators[key] = accumulator
		}

		sideEffect := classifyInteraction(interaction)
		accumulator.action.SideEffect = higherRisk(accumulator.action.SideEffect, sideEffect)
		for _, tier := range fallbackTiersFromInteraction(interaction) {
			if !slicesContainsFallback(accumulator.action.AllowedFallbackTiers, tier) {
				accumulator.action.AllowedFallbackTiers = append(accumulator.action.AllowedFallbackTiers, tier)
			}
		}
		sortFallbackTiers(accumulator.action.AllowedFallbackTiers)

		if amount, ok := observedAmount(interaction.Request.Body); ok {
			if accumulator.action.MaxAmount == nil || amount > *accumulator.action.MaxAmount {
				accumulator.action.MaxAmount = &amount
			}
		}
		for _, model := range observedModels(interaction.Request.Body) {
			models[model] = true
			accumulator.models[model] = true
		}
	}

	keys := make([]string, 0, len(accumulators))
	for key := range accumulators {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	file := File{
		SchemaVersion: SchemaVersion,
		Agent: Agent{
			Name:   agentName,
			Models: sortedStrings(models),
		},
	}
	summary := GenerateSummary{
		Models: sortedStrings(models),
	}

	for _, key := range keys {
		action := accumulators[key].action
		if action.SideEffect == "" {
			action.SideEffect = SideEffectUnknown
		}
		switch action.SideEffect {
		case SideEffectRead:
			file.AllowedActions = append(file.AllowedActions, action)
			summary.AllowedActions++
		default:
			action.RequiresApproval = requiresApproval(action.SideEffect)
			file.RestrictedActions = append(file.RestrictedActions, action)
			summary.RestrictedActions++
			if action.SideEffect == SideEffectUnknown {
				summary.UnknownRisk++
			}
		}
	}

	return file, summary
}

func RenderYAML(file File, sourceRunID, baselineID string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Stagehand behavior contract generated from approved baseline.\n")
	if strings.TrimSpace(sourceRunID) != "" {
		fmt.Fprintf(&b, "# Source run: %s\n", sourceRunID)
	}
	if strings.TrimSpace(baselineID) != "" {
		fmt.Fprintf(&b, "# Baseline: %s\n", baselineID)
	}
	fmt.Fprintf(&b, "# Review side_effect, fallback tiers, approval requirements, and limits before committing.\n\n")
	fmt.Fprintf(&b, "schema_version: %s\n\n", SchemaVersion)
	fmt.Fprintf(&b, "agent:\n")
	fmt.Fprintf(&b, "  name: %s\n", yamlString(file.Agent.Name))
	if len(file.Agent.Models) > 0 {
		fmt.Fprintf(&b, "  # Models observed in the baseline run.\n")
		fmt.Fprintf(&b, "  models:\n")
		for _, model := range file.Agent.Models {
			fmt.Fprintf(&b, "    - %s\n", yamlString(model))
		}
	}
	fmt.Fprintf(&b, "\n")

	if len(file.AllowedActions) > 0 {
		fmt.Fprintf(&b, "allowed_actions:\n")
		fmt.Fprintf(&b, "  # Review: these actions were classified as read-only baseline behavior.\n")
		renderActions(&b, file.AllowedActions)
		fmt.Fprintf(&b, "\n")
	}
	if len(file.RestrictedActions) > 0 {
		fmt.Fprintf(&b, "restricted_actions:\n")
		fmt.Fprintf(&b, "  # Review carefully: these actions write, send messages, move money, destroy data, or have unknown risk.\n")
		renderActions(&b, file.RestrictedActions)
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "# Add forbidden_actions for behavior that must never be allowed, for example:\n")
	fmt.Fprintf(&b, "# forbidden_actions:\n")
	fmt.Fprintf(&b, "#   - service: postgres\n")
	fmt.Fprintf(&b, "#     operation: DELETE\n")
	fmt.Fprintf(&b, "#     side_effect: destructive\n")
	fmt.Fprintf(&b, "#     reason: destructive database write\n")

	return []byte(b.String())
}

func renderActions(b *strings.Builder, actions []Action) {
	for _, action := range actions {
		if strings.TrimSpace(action.Tool) != "" {
			fmt.Fprintf(b, "  - tool: %s\n", yamlString(action.Tool))
		} else {
			fmt.Fprintf(b, "  - service: %s\n", yamlString(action.Service))
			fmt.Fprintf(b, "    operation: %s\n", yamlString(action.Operation))
		}
		fmt.Fprintf(b, "    side_effect: %s\n", action.SideEffect)
		if len(action.AllowedFallbackTiers) > 0 {
			fmt.Fprintf(b, "    allowed_fallback_tiers:\n")
			for _, tier := range action.AllowedFallbackTiers {
				fmt.Fprintf(b, "      - %s\n", tier)
			}
		}
		if action.MaxAmount != nil {
			fmt.Fprintf(b, "    max_amount: %s\n", yamlNumber(*action.MaxAmount))
		}
		if action.RequiresApproval {
			fmt.Fprintf(b, "    requires_approval: true\n")
		}
	}
}

func selectorFromInteraction(interaction recorder.Interaction) (ActionSelector, bool) {
	if interaction.Protocol == recorder.ProtocolTool || interaction.Service == "stagehand.tool" {
		toolName := strings.TrimSpace(interaction.Operation)
		if toolName == "" {
			toolName = stringFromMap(interaction.Request.Body, "name")
		}
		if toolName == "" {
			return ActionSelector{}, false
		}
		return ActionSelector{Tool: toolName}, true
	}

	service := strings.TrimSpace(interaction.Service)
	operation := strings.TrimSpace(interaction.Operation)
	if service == "" || operation == "" {
		return ActionSelector{}, false
	}
	return ActionSelector{Service: service, Operation: operation}, true
}

func classifyInteraction(interaction recorder.Interaction) SideEffect {
	if interaction.Protocol == recorder.ProtocolTool || interaction.Service == "stagehand.tool" {
		if sideEffect := sideEffectFromValue(valueFromMap(interaction.Request.Body, "side_effect")); sideEffect != "" {
			return sideEffect
		}
		for _, event := range interaction.Events {
			if sideEffect := sideEffectFromValue(event.Data["side_effect"]); sideEffect != "" {
				return sideEffect
			}
		}
		return inferRiskFromText(interaction.Operation)
	}

	if strings.EqualFold(interaction.Service, "openai") || strings.EqualFold(interaction.Service, "anthropic") {
		return SideEffectRead
	}
	if interaction.Protocol == recorder.ProtocolPostgres {
		return classifyDatabaseOperation(interaction.Operation)
	}

	text := strings.ToLower(strings.Join([]string{
		interaction.Service,
		interaction.Operation,
		interaction.Request.Method,
		interaction.Request.URL,
	}, " "))
	if sideEffect := keywordRisk(text); sideEffect != "" {
		return sideEffect
	}

	switch strings.ToUpper(strings.TrimSpace(interaction.Request.Method)) {
	case "GET", "HEAD", "OPTIONS":
		return SideEffectRead
	case "DELETE":
		return SideEffectDestructive
	case "POST", "PUT", "PATCH":
		return SideEffectWrite
	}
	if strings.TrimSpace(interaction.Request.Method) == "" {
		return inferRiskFromText(interaction.Operation)
	}
	return SideEffectUnknown
}

func classifyDatabaseOperation(operation string) SideEffect {
	verb := firstWord(operation)
	switch strings.ToUpper(verb) {
	case "SELECT", "SHOW", "DESCRIBE", "EXPLAIN":
		return SideEffectRead
	case "INSERT", "UPDATE", "UPSERT":
		return SideEffectWrite
	case "DELETE", "DROP", "TRUNCATE", "ALTER":
		return SideEffectDestructive
	default:
		return SideEffectUnknown
	}
}

func inferRiskFromText(value string) SideEffect {
	if sideEffect := keywordRisk(strings.ToLower(value)); sideEffect != "" {
		return sideEffect
	}
	return SideEffectUnknown
}

func keywordRisk(text string) SideEffect {
	for _, term := range []string{"delete", "drop", "remove", "archive", "disable", "destroy", "truncate", "alter"} {
		if strings.Contains(text, term) {
			return SideEffectDestructive
		}
	}
	for _, term := range []string{"refund", "charge", "payment", "invoice", "payout", "transfer"} {
		if strings.Contains(text, term) {
			return SideEffectFinancial
		}
	}
	for _, term := range []string{"email", "sms", "message", "notify", "slack", "send"} {
		if strings.Contains(text, term) {
			return SideEffectExternalMessage
		}
	}
	return ""
}

func requiresApproval(sideEffect SideEffect) bool {
	switch sideEffect {
	case SideEffectFinancial, SideEffectDestructive, SideEffectExternalMessage, SideEffectUnknown:
		return true
	default:
		return false
	}
}

func higherRisk(current, candidate SideEffect) SideEffect {
	if riskRank(candidate) > riskRank(current) {
		return candidate
	}
	return current
}

func riskRank(sideEffect SideEffect) int {
	switch sideEffect {
	case SideEffectRead:
		return 1
	case SideEffectWrite:
		return 2
	case SideEffectExternalMessage:
		return 3
	case SideEffectFinancial:
		return 4
	case SideEffectDestructive:
		return 5
	case SideEffectUnknown:
		return 6
	default:
		return 0
	}
}

func fallbackTiersFromInteraction(interaction recorder.Interaction) []FallbackTier {
	if interaction.FallbackTier == "" {
		return nil
	}
	switch interaction.FallbackTier {
	case recorder.FallbackTierExact:
		return []FallbackTier{FallbackTierExact}
	case recorder.FallbackTierNearestNeighbor:
		return []FallbackTier{FallbackTierNearestNeighbor}
	case recorder.FallbackTierStateSynthesis:
		return []FallbackTier{FallbackTierStateSynthesis}
	case recorder.FallbackTierLLMSynthesis:
		return []FallbackTier{FallbackTierLLMSynthesis}
	default:
		return nil
	}
}

func observedModels(value any) []string {
	models := map[string]bool{}
	collectModels(value, models)
	return sortedStrings(models)
}

func collectModels(value any, models map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if strings.EqualFold(key, "model") {
				if model := strings.TrimSpace(fmt.Sprint(item)); model != "" {
					models[model] = true
				}
				continue
			}
			collectModels(item, models)
		}
	case []any:
		for _, item := range typed {
			collectModels(item, models)
		}
	}
}

func observedAmount(value any) (float64, bool) {
	return findNumericField(value, "amount")
}

func findNumericField(value any, target string) (float64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if strings.EqualFold(key, target) {
				return numericValue(item)
			}
			if amount, ok := findNumericField(item, target); ok {
				return amount, true
			}
		}
	case []any:
		for _, item := range typed {
			if amount, ok := findNumericField(item, target); ok {
				return amount, true
			}
		}
	}
	return 0, false
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func sideEffectFromValue(value any) SideEffect {
	if value == nil {
		return ""
	}
	candidate := SideEffect(strings.TrimSpace(fmt.Sprint(value)))
	if candidate == "" {
		return ""
	}
	for _, valid := range validSideEffects {
		if candidate == valid {
			return candidate
		}
	}
	return SideEffectUnknown
}

func valueFromMap(value any, key string) any {
	if typed, ok := value.(map[string]any); ok {
		return typed[key]
	}
	return nil
}

func stringFromMap(value any, key string) string {
	raw := valueFromMap(value, key)
	if raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func selectorKey(selector ActionSelector) string {
	if strings.TrimSpace(selector.Tool) != "" {
		return "tool:" + strings.TrimSpace(selector.Tool)
	}
	return "service:" + strings.TrimSpace(selector.Service) + "\noperation:" + strings.TrimSpace(selector.Operation)
}

func sortedStrings(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func slicesContainsFallback(values []FallbackTier, target FallbackTier) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sortFallbackTiers(values []FallbackTier) {
	sort.Slice(values, func(i, j int) bool {
		return fallbackTierIndex(values[i]) < fallbackTierIndex(values[j])
	})
}

func fallbackTierIndex(value FallbackTier) int {
	for idx, candidate := range validFallbackTiers {
		if value == candidate {
			return idx
		}
	}
	return int(^uint(0) >> 1)
}

func firstWord(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func yamlString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Scheme != "" {
		return strconv.Quote(value)
	}
	for _, r := range value {
		if !(r == '-' || r == '_' || r == '.' || r == '/' || r == ':' || r == '@' || r == ' ' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return strconv.Quote(value)
		}
	}
	return value
}

func yamlNumber(value float64) string {
	if math.Trunc(value) == value {
		return fmt.Sprintf("%.0f", value)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
