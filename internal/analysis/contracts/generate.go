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
	AgentName               string
	ClassificationOverrides []ClassificationOverride
}

type ClassificationOverride struct {
	Tool       string
	SideEffect SideEffect
	Reason     string
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

type Classification struct {
	SideEffect SideEffect `json:"side_effect"`
	Reason     string     `json:"reason"`
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

		classification := ClassifyInteractionWithOverrides(interaction, opts.ClassificationOverrides)
		if riskRank(classification.SideEffect) > riskRank(accumulator.action.SideEffect) || strings.TrimSpace(accumulator.action.ClassifierReason) == "" {
			accumulator.action.ClassifierReason = classification.Reason
		}
		accumulator.action.SideEffect = higherRisk(accumulator.action.SideEffect, classification.SideEffect)
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
		renderGroupedActions(&b, file.AllowedActions)
		fmt.Fprintf(&b, "\n")
	}
	if len(file.RestrictedActions) > 0 {
		fmt.Fprintf(&b, "restricted_actions:\n")
		fmt.Fprintf(&b, "  # Review carefully: these actions write, send messages, move money, destroy data, or have unknown risk.\n")
		renderGroupedActions(&b, file.RestrictedActions)
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

func renderGroupedActions(b *strings.Builder, actions []Action) {
	groups := groupActions(actions)
	for _, group := range groups {
		fmt.Fprintf(b, "  # %s\n", group.label)
		for _, action := range group.actions {
			renderAction(b, action)
		}
	}
}

type actionGroup struct {
	label   string
	sortKey string
	actions []Action
}

func groupActions(actions []Action) []actionGroup {
	groupByKey := map[string]*actionGroup{}
	for _, action := range actions {
		label, sortKey := actionGroupLabel(action)
		group := groupByKey[sortKey]
		if group == nil {
			group = &actionGroup{
				label:   label,
				sortKey: sortKey,
			}
			groupByKey[sortKey] = group
		}
		group.actions = append(group.actions, action)
	}

	groups := make([]actionGroup, 0, len(groupByKey))
	for _, group := range groupByKey {
		sort.Slice(group.actions, func(i, j int) bool {
			return actionSortKey(group.actions[i]) < actionSortKey(group.actions[j])
		})
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].sortKey < groups[j].sortKey
	})
	return groups
}

func actionGroupLabel(action Action) (string, string) {
	if strings.TrimSpace(action.Tool) != "" {
		return "Tools", "1:tool"
	}
	service := strings.TrimSpace(action.Service)
	if service == "" {
		return "Service: unknown", "0:"
	}
	return "Service: " + service, "0:" + service
}

func actionSortKey(action Action) string {
	if strings.TrimSpace(action.Tool) != "" {
		return strings.TrimSpace(action.Tool)
	}
	return strings.TrimSpace(action.Operation)
}

func renderAction(b *strings.Builder, action Action) {
	fmt.Fprintf(b, "  # Suggested risk: %s\n", action.SideEffect)
	if gate := suggestedGateComment(action); gate != "" {
		fmt.Fprintf(b, "  # Suggested release gate: %s\n", gate)
	}
	if action.SideEffect == SideEffectUnknown {
		fmt.Fprintf(b, "  # UNKNOWN RISK: review and replace side_effect before approving this action.\n")
	}
	if strings.TrimSpace(action.Tool) != "" {
		fmt.Fprintf(b, "  - tool: %s\n", yamlString(action.Tool))
	} else {
		fmt.Fprintf(b, "  - service: %s\n", yamlString(action.Service))
		fmt.Fprintf(b, "    operation: %s\n", yamlString(action.Operation))
	}
	fmt.Fprintf(b, "    side_effect: %s\n", action.SideEffect)
	if strings.TrimSpace(action.ClassifierReason) != "" {
		fmt.Fprintf(b, "    classifier_reason: %s\n", yamlString(action.ClassifierReason))
	}
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

func suggestedGateComment(action Action) string {
	switch action.SideEffect {
	case SideEffectFinancial:
		return "require approval and a reviewed max_amount before this financial action runs."
	case SideEffectDestructive:
		return "block unless an explicit destructive-action approval is present."
	case SideEffectExternalMessage:
		return "require approval before sending external customer messages."
	case SideEffectUnknown:
		return "forbid unknown-risk actions until side_effect is reviewed."
	case SideEffectWrite:
		return "review whether writes should require approval or ordering constraints."
	default:
		return ""
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
	return ClassifyInteraction(interaction).SideEffect
}

func ClassifyInteractionWithOverrides(interaction recorder.Interaction, overrides []ClassificationOverride) Classification {
	if override, ok := matchingClassificationOverride(interaction, overrides); ok {
		reason := strings.TrimSpace(override.Reason)
		if reason == "" {
			reason = "config classification.tool_overrides declares side_effect=" + string(override.SideEffect)
		}
		return Classification{SideEffect: override.SideEffect, Reason: reason}
	}
	return ClassifyInteraction(interaction)
}

func ClassifyInteraction(interaction recorder.Interaction) Classification {
	if interaction.Protocol == recorder.ProtocolTool || interaction.Service == "stagehand.tool" {
		if sideEffect := sideEffectFromValue(valueFromMap(interaction.Request.Body, "side_effect")); sideEffect != "" {
			return Classification{SideEffect: sideEffect, Reason: "tool request body declares side_effect=" + string(sideEffect)}
		}
		for _, event := range interaction.Events {
			if sideEffect := sideEffectFromValue(event.Data["side_effect"]); sideEffect != "" {
				return Classification{SideEffect: sideEffect, Reason: "tool event declares side_effect=" + string(sideEffect)}
			}
		}
		return classifyToolName(toolNameFromInteraction(interaction))
	}

	if strings.EqualFold(interaction.Service, "openai") || strings.EqualFold(interaction.Service, "anthropic") {
		return Classification{SideEffect: SideEffectRead, Reason: fmt.Sprintf("service %s is treated as model read", interaction.Service)}
	}
	if interaction.Protocol == recorder.ProtocolPostgres {
		return classifyDatabaseInteraction(interaction)
	}

	method := normalizedHTTPMethod(interaction)
	if method == "DELETE" {
		return classifyHTTPMethod(method)
	}

	text := strings.ToLower(strings.Join([]string{
		interaction.Service,
		interaction.Operation,
		interaction.Request.Method,
		interaction.Request.URL,
	}, " "))
	if classification := keywordRisk(text); classification.SideEffect != "" {
		return classification
	}

	if classification := classifyHTTPMethod(method); classification.SideEffect != "" {
		return classification
	}
	if strings.TrimSpace(interaction.Request.Method) == "" {
		return inferRiskFromText(interaction.Operation)
	}
	return Classification{SideEffect: SideEffectUnknown, Reason: "HTTP method " + method + " did not match known risk rules"}
}

func matchingClassificationOverride(interaction recorder.Interaction, overrides []ClassificationOverride) (ClassificationOverride, bool) {
	if len(overrides) == 0 {
		return ClassificationOverride{}, false
	}
	if interaction.Protocol != recorder.ProtocolTool && interaction.Service != "stagehand.tool" {
		return ClassificationOverride{}, false
	}
	toolName := toolNameFromInteraction(interaction)
	for _, override := range overrides {
		if strings.EqualFold(strings.TrimSpace(override.Tool), toolName) && override.SideEffect != "" {
			return override, true
		}
	}
	return ClassificationOverride{}, false
}

func toolNameFromInteraction(interaction recorder.Interaction) string {
	toolName := strings.TrimSpace(interaction.Operation)
	if toolName == "" {
		toolName = stringFromMap(interaction.Request.Body, "name")
	}
	return toolName
}

func classifyToolName(toolName string) Classification {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return Classification{SideEffect: SideEffectUnknown, Reason: "tool has no side_effect metadata and no name to classify"}
	}
	if classification := keywordRisk(strings.ToLower(trimmed)); classification.SideEffect != "" {
		classification.Reason = "tool name " + classification.Reason
		return classification
	}
	if classification := toolVerbRisk(trimmed); classification.SideEffect != "" {
		return classification
	}
	return Classification{SideEffect: SideEffectUnknown, Reason: "tool has no side_effect metadata and name did not match known risk rules"}
}

func toolVerbRisk(toolName string) Classification {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(toolName, "-", "_"), " ", "_"))
	parts := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == '_' || r == '.' || r == ':' || r == '/'
	})
	for _, part := range parts {
		switch part {
		case "get", "lookup", "list", "find", "search", "fetch", "read", "retrieve":
			return Classification{SideEffect: SideEffectRead, Reason: fmt.Sprintf("tool name contains read term %q", part)}
		case "update", "create", "set", "assign", "add", "edit", "write", "upsert", "close", "open":
			return Classification{SideEffect: SideEffectWrite, Reason: fmt.Sprintf("tool name contains write term %q", part)}
		}
	}
	return Classification{}
}

func normalizedHTTPMethod(interaction recorder.Interaction) string {
	method := strings.ToUpper(strings.TrimSpace(interaction.Request.Method))
	if method == "" {
		method = strings.ToUpper(firstWord(interaction.Operation))
	}
	return method
}

func classifyHTTPMethod(method string) Classification {
	switch method {
	case "GET", "HEAD":
		return Classification{SideEffect: SideEffectRead, Reason: "HTTP method " + method + " classified as read"}
	case "OPTIONS":
		return Classification{SideEffect: SideEffectRead, Reason: "HTTP method OPTIONS classified as safe read"}
	case "DELETE":
		return Classification{SideEffect: SideEffectDestructive, Reason: "HTTP method DELETE classified as destructive"}
	case "POST", "PUT", "PATCH":
		return Classification{SideEffect: SideEffectWrite, Reason: "HTTP method " + method + " classified as write"}
	}
	return Classification{}
}

func classifyDatabaseInteraction(interaction recorder.Interaction) Classification {
	query := databaseQueryText(interaction)
	classification := classifyDatabaseOperation(query)
	if snippet := scrubbedQuerySnippet(interaction, query); snippet != "" {
		classification.Reason += "; scrubbed query snippet: " + strconv.Quote(snippet)
	}
	return classification
}

func classifyDatabaseOperation(query string) Classification {
	verb := sqlVerb(query)
	switch strings.ToUpper(verb) {
	case "SELECT", "SHOW", "DESCRIBE", "EXPLAIN":
		return Classification{SideEffect: SideEffectRead, Reason: "database operation " + strings.ToUpper(verb) + " classified as read"}
	case "INSERT", "UPDATE", "UPSERT":
		return Classification{SideEffect: SideEffectWrite, Reason: "database operation " + strings.ToUpper(verb) + " classified as write"}
	case "DELETE", "DROP", "TRUNCATE", "ALTER":
		return Classification{SideEffect: SideEffectDestructive, Reason: "database operation " + strings.ToUpper(verb) + " classified as destructive"}
	default:
		return Classification{SideEffect: SideEffectUnknown, Reason: "database operation did not match known risk rules"}
	}
}

func databaseQueryText(interaction recorder.Interaction) string {
	for _, value := range []string{
		strings.TrimSpace(interaction.Operation),
		stringFromNestedMap(interaction.Request.Body, "query"),
		stringFromNestedMap(interaction.Request.Body, "sql"),
		stringFromNestedMap(interaction.Request.Body, "statement"),
		stringFromNestedMap(interaction.Request.Body, "command"),
	} {
		if isKnownSQLVerb(sqlVerb(value)) {
			return value
		}
	}
	return strings.TrimSpace(interaction.Operation)
}

func sqlVerb(query string) string {
	query = strings.TrimSpace(query)
	for {
		switch {
		case strings.HasPrefix(query, "--"):
			newline := strings.Index(query, "\n")
			if newline < 0 {
				return ""
			}
			query = strings.TrimSpace(query[newline+1:])
		case strings.HasPrefix(query, "/*"):
			end := strings.Index(query, "*/")
			if end < 0 {
				return ""
			}
			query = strings.TrimSpace(query[end+2:])
		default:
			return firstWord(query)
		}
	}
}

func isKnownSQLVerb(verb string) bool {
	switch strings.ToUpper(strings.TrimSpace(verb)) {
	case "SELECT", "SHOW", "DESCRIBE", "EXPLAIN", "INSERT", "UPDATE", "UPSERT", "DELETE", "DROP", "TRUNCATE", "ALTER":
		return true
	default:
		return false
	}
}

func scrubbedQuerySnippet(interaction recorder.Interaction, query string) string {
	if !scrubReportPresent(interaction.ScrubReport) {
		return ""
	}
	query = strings.Join(strings.Fields(query), " ")
	const limit = 120
	if len(query) <= limit {
		return query
	}
	return query[:limit] + "..."
}

func scrubReportPresent(report recorder.ScrubReport) bool {
	return strings.TrimSpace(report.ScrubPolicyVersion) != "" && strings.TrimSpace(report.SessionSaltID) != ""
}

func inferRiskFromText(value string) Classification {
	if classification := keywordRisk(strings.ToLower(value)); classification.SideEffect != "" {
		return classification
	}
	return Classification{SideEffect: SideEffectUnknown, Reason: "text did not match known risk terms"}
}

func keywordRisk(text string) Classification {
	for _, term := range []string{"delete", "drop", "remove", "archive", "disable", "destroy", "truncate", "alter"} {
		if strings.Contains(text, term) {
			return Classification{SideEffect: SideEffectDestructive, Reason: fmt.Sprintf("endpoint contains destructive term %q", term)}
		}
	}
	for _, term := range []string{"refund", "charge", "payment", "invoice", "payout", "transfer"} {
		if strings.Contains(text, term) {
			return Classification{SideEffect: SideEffectFinancial, Reason: fmt.Sprintf("endpoint contains financial term %q", term)}
		}
	}
	for _, term := range []string{"email", "sms", "message", "notify", "slack", "send"} {
		if strings.Contains(text, term) {
			return Classification{SideEffect: SideEffectExternalMessage, Reason: fmt.Sprintf("endpoint contains external-message term %q", term)}
		}
	}
	return Classification{}
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

func stringFromNestedMap(value any, key string) string {
	switch typed := value.(type) {
	case map[string]any:
		for itemKey, item := range typed {
			if strings.EqualFold(itemKey, key) {
				return strings.TrimSpace(fmt.Sprint(item))
			}
			if nested := stringFromNestedMap(item, key); nested != "" {
				return nested
			}
		}
	case []any:
		for _, item := range typed {
			if nested := stringFromNestedMap(item, key); nested != "" {
				return nested
			}
		}
	}
	return ""
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
