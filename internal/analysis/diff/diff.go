// Package diff aligns Stagehand runs and reports structured interaction changes.
package diff

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"stagehand/internal/recorder"
)

type ChangeType string

const (
	ChangeAdded              ChangeType = "added"
	ChangeRemoved            ChangeType = "removed"
	ChangeModified           ChangeType = "modified"
	ChangeOrderingChanged    ChangeType = "ordering_changed"
	ChangeFallbackRegression ChangeType = "fallback_regression"
)

type Options struct {
	IgnoredFields []string
}

type Result struct {
	BaseRunID      string
	CandidateRunID string
	SessionName    string
	Changes        []Change
}

type Change struct {
	Type                   ChangeType            `json:"type"`
	Failing                bool                  `json:"failing"`
	BaseInteractionID      string                `json:"base_interaction_id,omitempty"`
	CandidateInteractionID string                `json:"candidate_interaction_id,omitempty"`
	BaseSequence           int                   `json:"base_sequence,omitempty"`
	CandidateSequence      int                   `json:"candidate_sequence,omitempty"`
	Service                string                `json:"service,omitempty"`
	Operation              string                `json:"operation,omitempty"`
	BaseFallbackTier       recorder.FallbackTier `json:"base_fallback_tier,omitempty"`
	CandidateFallbackTier  recorder.FallbackTier `json:"candidate_fallback_tier,omitempty"`
	IgnoredFields          []string              `json:"ignored_fields,omitempty"`
	Details                string                `json:"details,omitempty"`
}

type alignedInteraction struct {
	key       string
	base      *recorder.Interaction
	candidate *recorder.Interaction
}

type indexedInteraction struct {
	key         string
	sequence    int
	interaction recorder.Interaction
}

func Compare(base, candidate recorder.Run, opts Options) (Result, error) {
	if strings.TrimSpace(base.SessionName) == "" || strings.TrimSpace(candidate.SessionName) == "" {
		return Result{}, fmt.Errorf("run session_name is required")
	}
	if base.SessionName != candidate.SessionName {
		return Result{}, fmt.Errorf("cannot diff runs from different sessions: %q != %q", base.SessionName, candidate.SessionName)
	}

	ignored := normalizeIgnoredFields(opts.IgnoredFields)
	aligned := alignInteractions(base.Interactions, candidate.Interactions, ignored)

	result := Result{
		BaseRunID:      base.RunID,
		CandidateRunID: candidate.RunID,
		SessionName:    base.SessionName,
		Changes:        make([]Change, 0),
	}

	for _, pair := range aligned {
		switch {
		case pair.base == nil && pair.candidate != nil:
			result.Changes = append(result.Changes, addedChange(*pair.candidate))
		case pair.base != nil && pair.candidate == nil:
			result.Changes = append(result.Changes, removedChange(*pair.base))
		case pair.base != nil && pair.candidate != nil:
			baseInteraction := *pair.base
			candidateInteraction := *pair.candidate
			if baseInteraction.Sequence != candidateInteraction.Sequence {
				result.Changes = append(result.Changes, orderingChange(baseInteraction, candidateInteraction))
			}
			if fallbackRegressed(baseInteraction.FallbackTier, candidateInteraction.FallbackTier) {
				result.Changes = append(result.Changes, fallbackRegressionChange(baseInteraction, candidateInteraction))
			}
			if canonicalInteraction(baseInteraction, ignored) != canonicalInteraction(candidateInteraction, ignored) {
				result.Changes = append(result.Changes, modifiedChange(baseInteraction, candidateInteraction, ignored))
			}
		}
	}

	return result, nil
}

func alignInteractions(base []recorder.Interaction, candidate []recorder.Interaction, ignored map[string]bool) []alignedInteraction {
	baseIndexed := indexInteractions(base, ignored)
	candidateIndexed := indexInteractions(candidate, ignored)

	byKey := map[string]*alignedInteraction{}
	order := make([]string, 0, len(baseIndexed)+len(candidateIndexed))
	for _, item := range baseIndexed {
		interaction := item.interaction
		byKey[item.key] = &alignedInteraction{key: item.key, base: &interaction}
		order = append(order, item.key)
	}
	for _, item := range candidateIndexed {
		interaction := item.interaction
		if pair, ok := byKey[item.key]; ok {
			pair.candidate = &interaction
			continue
		}
		byKey[item.key] = &alignedInteraction{key: item.key, candidate: &interaction}
		order = append(order, item.key)
	}

	sort.SliceStable(order, func(i, j int) bool {
		left := byKey[order[i]]
		right := byKey[order[j]]
		leftSeq := alignedSortSequence(left)
		rightSeq := alignedSortSequence(right)
		if leftSeq != rightSeq {
			return leftSeq < rightSeq
		}
		return order[i] < order[j]
	})

	aligned := make([]alignedInteraction, 0, len(order))
	for _, key := range order {
		aligned = append(aligned, *byKey[key])
	}
	return aligned
}

func indexInteractions(interactions []recorder.Interaction, ignored map[string]bool) []indexedInteraction {
	ordered := append([]recorder.Interaction(nil), interactions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Sequence == ordered[j].Sequence {
			return ordered[i].InteractionID < ordered[j].InteractionID
		}
		return ordered[i].Sequence < ordered[j].Sequence
	})

	seen := map[string]int{}
	indexed := make([]indexedInteraction, 0, len(ordered))
	for _, interaction := range ordered {
		baseKey := identityKey(interaction, ignored)
		seen[baseKey]++
		key := fmt.Sprintf("%s\x00%d", baseKey, seen[baseKey])
		indexed = append(indexed, indexedInteraction{
			key:         key,
			sequence:    interaction.Sequence,
			interaction: interaction,
		})
	}
	return indexed
}

func identityKey(interaction recorder.Interaction, ignored map[string]bool) string {
	identity := map[string]any{
		"service":   interaction.Service,
		"operation": interaction.Operation,
		"protocol":  interaction.Protocol,
		"request": map[string]any{
			"method": strings.ToUpper(interaction.Request.Method),
			"url":    normalizedURL(interaction.Request.URL),
		},
	}
	removeIgnoredFields(identity, ignored)
	return canonicalJSON(identity)
}

func canonicalInteraction(interaction recorder.Interaction, ignored map[string]bool) string {
	encoded, err := json.Marshal(interaction)
	if err != nil {
		return fmt.Sprintf("%#v", interaction)
	}

	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return string(encoded)
	}
	if object, ok := decoded.(map[string]any); ok {
		removeIgnoredFields(object, ignored)
	}
	return canonicalJSON(decoded)
}

func normalizedURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimSpace(raw)
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	values := parsed.Query()
	keys := make([]string, 0, len(values))
	for key := range values {
		sort.Strings(values[key])
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := url.Values{}
	for _, key := range keys {
		for _, value := range values[key] {
			ordered.Add(key, value)
		}
	}
	parsed.RawQuery = ordered.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func removeIgnoredFields(value any, ignored map[string]bool) {
	for path := range ignored {
		removePath(value, strings.Split(path, "."))
	}
}

func removePath(value any, parts []string) {
	if len(parts) == 0 {
		return
	}
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	if len(parts) == 1 {
		delete(object, parts[0])
		return
	}
	removePath(object[parts[0]], parts[1:])
}

func canonicalJSON(value any) string {
	encoded, err := json.Marshal(normalizeValue(value))
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return string(encoded)
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := map[string]any{}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			normalized[key] = normalizeValue(typed[key])
		}
		return normalized
	case map[string][]string:
		normalized := map[string]any{}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values := append([]string(nil), typed[key]...)
			sort.Strings(values)
			normalized[strings.ToLower(key)] = values
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for idx, item := range typed {
			normalized[idx] = normalizeValue(item)
		}
		return normalized
	case []recorder.Event:
		normalized := make([]any, len(typed))
		for idx, item := range typed {
			normalized[idx] = normalizeValue(item)
		}
		return normalized
	default:
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
}

func normalizeIgnoredFields(fields []string) map[string]bool {
	ignored := map[string]bool{
		"run_id":         true,
		"interaction_id": true,
		"sequence":       true,
		"fallback_tier":  true,
	}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		field = strings.TrimPrefix(field, "$.")
		field = strings.TrimPrefix(field, ".")
		if field == "" {
			continue
		}
		ignored[field] = true
	}
	return ignored
}

func fallbackRegressed(base, candidate recorder.FallbackTier) bool {
	return fallbackRank(candidate) > fallbackRank(base)
}

func fallbackRank(tier recorder.FallbackTier) int {
	switch tier {
	case "":
		return 0
	case recorder.FallbackTierExact:
		return 0
	case recorder.FallbackTierNearestNeighbor:
		return 1
	case recorder.FallbackTierStateSynthesis:
		return 2
	case recorder.FallbackTierLLMSynthesis:
		return 3
	default:
		return 4
	}
}

func alignedSortSequence(pair *alignedInteraction) int {
	switch {
	case pair.base != nil:
		return pair.base.Sequence
	case pair.candidate != nil:
		return pair.candidate.Sequence
	default:
		return 0
	}
}

func addedChange(interaction recorder.Interaction) Change {
	return Change{
		Type:                   ChangeAdded,
		Failing:                true,
		CandidateInteractionID: interaction.InteractionID,
		CandidateSequence:      interaction.Sequence,
		Service:                interaction.Service,
		Operation:              interaction.Operation,
		Details:                "interaction added in candidate run",
	}
}

func removedChange(interaction recorder.Interaction) Change {
	return Change{
		Type:              ChangeRemoved,
		Failing:           true,
		BaseInteractionID: interaction.InteractionID,
		BaseSequence:      interaction.Sequence,
		Service:           interaction.Service,
		Operation:         interaction.Operation,
		Details:           "interaction removed from candidate run",
	}
}

func orderingChange(base, candidate recorder.Interaction) Change {
	return Change{
		Type:                   ChangeOrderingChanged,
		Failing:                false,
		BaseInteractionID:      base.InteractionID,
		CandidateInteractionID: candidate.InteractionID,
		BaseSequence:           base.Sequence,
		CandidateSequence:      candidate.Sequence,
		Service:                candidate.Service,
		Operation:              candidate.Operation,
		Details:                "matched interaction sequence changed",
	}
}

func fallbackRegressionChange(base, candidate recorder.Interaction) Change {
	return Change{
		Type:                   ChangeFallbackRegression,
		Failing:                true,
		BaseInteractionID:      base.InteractionID,
		CandidateInteractionID: candidate.InteractionID,
		BaseSequence:           base.Sequence,
		CandidateSequence:      candidate.Sequence,
		Service:                candidate.Service,
		Operation:              candidate.Operation,
		BaseFallbackTier:       base.FallbackTier,
		CandidateFallbackTier:  candidate.FallbackTier,
		Details:                "candidate used a less exact fallback tier",
	}
}

func modifiedChange(base, candidate recorder.Interaction, ignored map[string]bool) Change {
	ignoredFields := make([]string, 0, len(ignored))
	for field := range ignored {
		ignoredFields = append(ignoredFields, field)
	}
	sort.Strings(ignoredFields)

	return Change{
		Type:                   ChangeModified,
		Failing:                true,
		BaseInteractionID:      base.InteractionID,
		CandidateInteractionID: candidate.InteractionID,
		BaseSequence:           base.Sequence,
		CandidateSequence:      candidate.Sequence,
		Service:                candidate.Service,
		Operation:              candidate.Operation,
		IgnoredFields:          ignoredFields,
		Details:                "matched interaction fields changed",
	}
}
