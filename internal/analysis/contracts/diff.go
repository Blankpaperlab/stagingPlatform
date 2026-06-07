package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"stagehand/internal/recorder"
)

type DiffChangeType string

const (
	DiffChangeNewAction     DiffChangeType = "new_action"
	DiffChangeRemovedAction DiffChangeType = "removed_action"
	DiffChangeSideEffect    DiffChangeType = "side_effect_changed"
	DiffChangeFallbackTiers DiffChangeType = "fallback_tiers_changed"
	DiffChangeModels        DiffChangeType = "models_changed"
	DiffChangePrompts       DiffChangeType = "prompts_changed"
)

type DiffSummary struct {
	TotalChanges        int `json:"total_changes"`
	NewActions          int `json:"new_actions"`
	RemovedActions      int `json:"removed_actions"`
	SideEffectChanges   int `json:"side_effect_changes"`
	FallbackTierChanges int `json:"fallback_tier_changes"`
	ModelChanges        int `json:"model_changes"`
	PromptChanges       int `json:"prompt_changes"`
}

type DiffResult struct {
	BaseRunID      string       `json:"base_run_id"`
	CandidateRunID string       `json:"candidate_run_id"`
	SessionName    string       `json:"session_name"`
	Summary        DiffSummary  `json:"summary"`
	Changes        []DiffChange `json:"changes"`
}

type DiffChange struct {
	Type                   DiffChangeType        `json:"type"`
	Service                string                `json:"service,omitempty"`
	Operation              string                `json:"operation,omitempty"`
	Tool                   string                `json:"tool,omitempty"`
	BaseSideEffect         SideEffect            `json:"base_side_effect,omitempty"`
	CandidateSideEffect    SideEffect            `json:"candidate_side_effect,omitempty"`
	BaseFallbackTiers      []FallbackTier        `json:"base_fallback_tiers,omitempty"`
	CandidateFallbackTiers []FallbackTier        `json:"candidate_fallback_tiers,omitempty"`
	BaseModels             []string              `json:"base_models,omitempty"`
	CandidateModels        []string              `json:"candidate_models,omitempty"`
	BasePrompts            []PromptDigest        `json:"base_prompts,omitempty"`
	CandidatePrompts       []PromptDigest        `json:"candidate_prompts,omitempty"`
	BaseEvidence           []InteractionEvidence `json:"base_evidence,omitempty"`
	CandidateEvidence      []InteractionEvidence `json:"candidate_evidence,omitempty"`
	Details                string                `json:"details,omitempty"`
}

type PromptDigest struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Preview string `json:"preview,omitempty"`
}

type actionSnapshot struct {
	selector      ActionSelector
	sideEffect    SideEffect
	fallbackTiers []FallbackTier
	models        []string
	prompts       []PromptDigest
	evidence      []InteractionEvidence
	firstSequence int
}

func DiffRuns(base, candidate recorder.Run) (DiffResult, error) {
	if strings.TrimSpace(base.SessionName) == "" || strings.TrimSpace(candidate.SessionName) == "" {
		return DiffResult{}, fmt.Errorf("run session_name is required")
	}
	if base.SessionName != candidate.SessionName {
		return DiffResult{}, fmt.Errorf("cannot contract-diff runs from different sessions: %q != %q", base.SessionName, candidate.SessionName)
	}

	baseActions := snapshotsFromRun(base)
	candidateActions := snapshotsFromRun(candidate)
	keys := unionSnapshotKeys(baseActions, candidateActions)

	result := DiffResult{
		BaseRunID:      base.RunID,
		CandidateRunID: candidate.RunID,
		SessionName:    base.SessionName,
	}
	for _, key := range keys {
		baseSnapshot, baseFound := baseActions[key]
		candidateSnapshot, candidateFound := candidateActions[key]
		switch {
		case !baseFound && candidateFound:
			result.Changes = append(result.Changes, snapshotChange(DiffChangeNewAction, nil, candidateSnapshot, "action appears only in candidate run"))
		case baseFound && !candidateFound:
			result.Changes = append(result.Changes, snapshotChange(DiffChangeRemovedAction, baseSnapshot, nil, "action appears only in base run"))
		case baseFound && candidateFound:
			result.Changes = append(result.Changes, compareSnapshots(baseSnapshot, candidateSnapshot)...)
		}
	}
	result.Summary = summarizeDiffChanges(result.Changes)
	return result, nil
}

func snapshotsFromRun(run recorder.Run) map[string]*actionSnapshot {
	snapshots := map[string]*actionSnapshot{}
	for _, interaction := range run.Interactions {
		selector, ok := selectorFromInteraction(interaction)
		if !ok {
			continue
		}
		key := selectorKey(selector)
		snapshot := snapshots[key]
		if snapshot == nil {
			snapshot = &actionSnapshot{
				selector:      selector,
				firstSequence: interaction.Sequence,
			}
			snapshots[key] = snapshot
		}
		if interaction.Sequence < snapshot.firstSequence {
			snapshot.firstSequence = interaction.Sequence
		}
		snapshot.sideEffect = higherRisk(snapshot.sideEffect, classifyInteraction(interaction))
		for _, tier := range fallbackTiersFromInteraction(interaction) {
			if !slicesContainsFallback(snapshot.fallbackTiers, tier) {
				snapshot.fallbackTiers = append(snapshot.fallbackTiers, tier)
			}
		}
		sortFallbackTiers(snapshot.fallbackTiers)
		snapshot.models = sortedStringSet(append(snapshot.models, observedModels(interaction.Request.Body)...))
		snapshot.prompts = mergePromptDigests(snapshot.prompts, promptDigests(interaction.Request.Body))
		snapshot.evidence = append(snapshot.evidence, evidenceFromInteraction(interaction, selector))
	}
	return snapshots
}

func compareSnapshots(base, candidate *actionSnapshot) []DiffChange {
	changes := []DiffChange{}
	if base.sideEffect != candidate.sideEffect {
		changes = append(changes, snapshotChange(DiffChangeSideEffect, base, candidate, fmt.Sprintf("side_effect changed from %s to %s", emptySideEffect(base.sideEffect), emptySideEffect(candidate.sideEffect))))
	}
	if !fallbackSlicesEqual(base.fallbackTiers, candidate.fallbackTiers) {
		changes = append(changes, snapshotChange(DiffChangeFallbackTiers, base, candidate, fmt.Sprintf("fallback tiers changed from %s to %s", fallbackList(base.fallbackTiers), fallbackList(candidate.fallbackTiers))))
	}
	if !stringSlicesEqual(base.models, candidate.models) {
		changes = append(changes, snapshotChange(DiffChangeModels, base, candidate, fmt.Sprintf("models changed from %s to %s", stringList(base.models), stringList(candidate.models))))
	}
	if !promptDigestSlicesEqual(base.prompts, candidate.prompts) {
		changes = append(changes, snapshotChange(DiffChangePrompts, base, candidate, "captured prompt fields changed"))
	}
	return changes
}

func snapshotChange(changeType DiffChangeType, base, candidate *actionSnapshot, details string) DiffChange {
	selected := candidate
	if selected == nil {
		selected = base
	}
	change := DiffChange{
		Type:    changeType,
		Details: details,
	}
	if selected != nil {
		change.Service = selected.selector.Service
		change.Operation = selected.selector.Operation
		change.Tool = selected.selector.Tool
	}
	if base != nil {
		change.BaseSideEffect = base.sideEffect
		change.BaseFallbackTiers = append([]FallbackTier(nil), base.fallbackTiers...)
		change.BaseModels = append([]string(nil), base.models...)
		change.BasePrompts = append([]PromptDigest(nil), base.prompts...)
		change.BaseEvidence = append([]InteractionEvidence(nil), base.evidence...)
	}
	if candidate != nil {
		change.CandidateSideEffect = candidate.sideEffect
		change.CandidateFallbackTiers = append([]FallbackTier(nil), candidate.fallbackTiers...)
		change.CandidateModels = append([]string(nil), candidate.models...)
		change.CandidatePrompts = append([]PromptDigest(nil), candidate.prompts...)
		change.CandidateEvidence = append([]InteractionEvidence(nil), candidate.evidence...)
	}
	return change
}

func summarizeDiffChanges(changes []DiffChange) DiffSummary {
	summary := DiffSummary{TotalChanges: len(changes)}
	for _, change := range changes {
		switch change.Type {
		case DiffChangeNewAction:
			summary.NewActions++
		case DiffChangeRemovedAction:
			summary.RemovedActions++
		case DiffChangeSideEffect:
			summary.SideEffectChanges++
		case DiffChangeFallbackTiers:
			summary.FallbackTierChanges++
		case DiffChangeModels:
			summary.ModelChanges++
		case DiffChangePrompts:
			summary.PromptChanges++
		}
	}
	return summary
}

func unionSnapshotKeys(base, candidate map[string]*actionSnapshot) []string {
	seen := map[string]bool{}
	keys := make([]string, 0, len(base)+len(candidate))
	for key := range base {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range candidate {
		if seen[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := snapshotSortSequence(keys[i], base, candidate)
		right := snapshotSortSequence(keys[j], base, candidate)
		if left != right {
			return left < right
		}
		return keys[i] < keys[j]
	})
	return keys
}

func snapshotSortSequence(key string, base, candidate map[string]*actionSnapshot) int {
	if snapshot := base[key]; snapshot != nil {
		return snapshot.firstSequence
	}
	if snapshot := candidate[key]; snapshot != nil {
		return snapshot.firstSequence
	}
	return 0
}

func promptDigests(value any) []PromptDigest {
	decoded := decodeJSONValue(value)
	digests := []PromptDigest{}
	collectPromptDigests("request.body", decoded, &digests)
	return mergePromptDigests(nil, digests)
}

func collectPromptDigests(path string, value any, digests *[]PromptDigest) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			item := typed[key]
			nextPath := path + "." + key
			if isPromptField(key) && hasPromptValue(item) {
				*digests = append(*digests, promptDigest(nextPath, item))
				continue
			}
			collectPromptDigests(nextPath, item, digests)
		}
	case []any:
		for idx, item := range typed {
			collectPromptDigests(fmt.Sprintf("%s[%d]", path, idx), item, digests)
		}
	}
}

func isPromptField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "messages", "prompt", "instructions", "input":
		return true
	default:
		return false
	}
}

func hasPromptValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return value != nil
	}
}

func promptDigest(path string, value any) PromptDigest {
	canonical := canonicalContractJSON(value)
	sum := sha256.Sum256([]byte(canonical))
	return PromptDigest{
		Path:    path,
		Hash:    "sha256:" + hex.EncodeToString(sum[:])[:16],
		Preview: promptPreview(value, canonical),
	}
}

func promptPreview(value any, canonical string) string {
	if text, ok := value.(string); ok {
		return truncatePromptPreview(text)
	}
	return truncatePromptPreview(canonical)
}

func truncatePromptPreview(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const limit = 120
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func mergePromptDigests(existing, candidates []PromptDigest) []PromptDigest {
	seen := map[string]bool{}
	merged := make([]PromptDigest, 0, len(existing)+len(candidates))
	for _, digest := range append(append([]PromptDigest(nil), existing...), candidates...) {
		key := digest.Path + "\n" + digest.Hash
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, digest)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Path != merged[j].Path {
			return merged[i].Path < merged[j].Path
		}
		return merged[i].Hash < merged[j].Hash
	})
	return merged
}

func decodeJSONValue(value any) any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return decoded
}

func canonicalContractJSON(value any) string {
	encoded, err := json.Marshal(normalizeContractValue(decodeJSONValue(value)))
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(encoded)
}

func normalizeContractValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := map[string]any{}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			normalized[key] = normalizeContractValue(typed[key])
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for idx, item := range typed {
			normalized[idx] = normalizeContractValue(item)
		}
		return normalized
	default:
		return typed
	}
}

func sortedStringSet(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	return sortedStrings(seen)
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func fallbackSlicesEqual(left, right []FallbackTier) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx] != right[idx] {
			return false
		}
	}
	return true
}

func promptDigestSlicesEqual(left, right []PromptDigest) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx].Path != right[idx].Path || left[idx].Hash != right[idx].Hash {
			return false
		}
	}
	return true
}

func emptySideEffect(value SideEffect) string {
	if value == "" {
		return "none"
	}
	return string(value)
}

func fallbackList(values []FallbackTier) string {
	if len(values) == 0 {
		return "none"
	}
	items := make([]string, len(values))
	for idx, value := range values {
		items[idx] = string(value)
	}
	return strings.Join(items, ",")
}

func stringList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ",")
}
