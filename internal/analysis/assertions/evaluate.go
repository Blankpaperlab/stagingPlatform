package assertions

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"stagehand/internal/recorder"
)

type ResultStatus string

const (
	ResultStatusPassed      ResultStatus = "passed"
	ResultStatusFailed      ResultStatus = "failed"
	ResultStatusUnsupported ResultStatus = "unsupported"
)

type Result struct {
	AssertionID string
	Type        AssertionType
	Status      ResultStatus
	Message     string
	Evidence    Evidence
}

type Evidence struct {
	MatchedInteractions []InteractionEvidence
	Violations          []InteractionEvidence
	LeftEntities        []EntityEvidence
	RightEntities       []EntityEvidence
	LinkedEntities      []EntityLinkEvidence
	Count               int
	Expected            any
	Actual              any
	Path                string
	Before              *InteractionEvidence
	After               *InteractionEvidence
	DisallowedTiers     []string
}

type InteractionEvidence struct {
	InteractionID string
	Sequence      int
	Service       string
	Operation     string
	FallbackTier  string
	EventSequence int
	EventType     string
}

type EntityEvidence struct {
	Interaction InteractionEvidence
	Path        string
	Value       any
	Found       bool
}

type EntityLinkEvidence struct {
	Left  EntityEvidence
	Right EntityEvidence
}

func Evaluate(run recorder.Run, file File) ([]Result, error) {
	if err := file.Validate(); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(file.Assertions))
	for _, assertion := range file.Assertions {
		results = append(results, evaluateAssertion(run, assertion))
	}
	return results, nil
}

func evaluateAssertion(run recorder.Run, assertion Assertion) Result {
	switch assertion.Type {
	case TypeCount:
		return evaluateCount(run, assertion)
	case TypeOrdering:
		return evaluateOrdering(run, assertion)
	case TypePayloadField:
		return evaluatePayloadField(run, assertion)
	case TypeForbiddenOperation:
		return evaluateForbiddenOperation(run, assertion)
	case TypeFallbackProhibition:
		return evaluateFallbackProhibition(run, assertion)
	case TypeCrossService:
		return evaluateCrossService(run, assertion)
	default:
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusUnsupported,
			Message:     fmt.Sprintf("assertion type %q is not executable", assertion.Type),
		}
	}
}

func evaluateCrossService(run recorder.Run, assertion Assertion) Result {
	leftEntities := entityMatches(run.Interactions, *assertion.Left)
	rightEntities := entityMatches(run.Interactions, *assertion.Right)
	links := equalEntityLinks(leftEntities, rightEntities)

	evidence := Evidence{
		LeftEntities:   leftEntities,
		RightEntities:  rightEntities,
		LinkedEntities: links,
		Count:          len(links),
		Expected:       map[string]any{"relationship": assertion.Relationship},
	}

	if len(leftEntities) == 0 || len(rightEntities) == 0 {
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusFailed,
			Message:     fmt.Sprintf("cross-service assertion %q failed: missing left or right entity references", assertion.ID),
			Evidence:    evidence,
		}
	}
	if len(links) == 0 {
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusFailed,
			Message:     fmt.Sprintf("cross-service assertion %q failed: no equal entity values linked %s to %s", assertion.ID, assertion.Left.Path, assertion.Right.Path),
			Evidence:    evidence,
		}
	}

	evidence.Actual = links[0].Left.Value
	return Result{
		AssertionID: assertion.ID,
		Type:        assertion.Type,
		Status:      ResultStatusPassed,
		Message:     fmt.Sprintf("cross-service assertion %q passed: found %d linked entity pair(s)", assertion.ID, len(links)),
		Evidence:    evidence,
	}
}

func evaluateCount(run recorder.Run, assertion Assertion) Result {
	matches := matchingInteractions(run.Interactions, assertion.Match)
	count := len(matches)
	expect := assertion.Expect.Count
	passed := true
	expected := map[string]any{}
	if expect.Equals != nil {
		expected["equals"] = *expect.Equals
		passed = count == *expect.Equals
	}
	if expect.Min != nil {
		expected["min"] = *expect.Min
		passed = passed && count >= *expect.Min
	}
	if expect.Max != nil {
		expected["max"] = *expect.Max
		passed = passed && count <= *expect.Max
	}

	status := ResultStatusFailed
	message := fmt.Sprintf("count assertion %q failed: matched %d interactions", assertion.ID, count)
	if passed {
		status = ResultStatusPassed
		message = fmt.Sprintf("count assertion %q passed: matched %d interactions", assertion.ID, count)
	}

	return Result{
		AssertionID: assertion.ID,
		Type:        assertion.Type,
		Status:      status,
		Message:     message,
		Evidence: Evidence{
			MatchedInteractions: interactionEvidenceList(matches),
			Count:               count,
			Expected:            expected,
			Actual:              count,
		},
	}
}

func evaluateOrdering(run recorder.Run, assertion Assertion) Result {
	beforeMatches := matchingInteractions(run.Interactions, *assertion.Before)
	afterMatches := matchingInteractions(run.Interactions, *assertion.After)

	var selectedBefore *recorder.Interaction
	var selectedAfter *recorder.Interaction
	for _, before := range beforeMatches {
		for _, after := range afterMatches {
			if before.Sequence < after.Sequence {
				selectedBefore = &before
				selectedAfter = &after
				break
			}
		}
		if selectedBefore != nil {
			break
		}
	}

	if selectedBefore == nil || selectedAfter == nil {
		message := fmt.Sprintf(
			"ordering assertion %q failed: no before/after match pair was found in order",
			assertion.ID,
		)
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusFailed,
			Message:     message,
			Evidence: Evidence{
				MatchedInteractions: append(interactionEvidenceList(beforeMatches), interactionEvidenceList(afterMatches)...),
				Count:               len(beforeMatches) + len(afterMatches),
			},
		}
	}

	beforeEvidence := interactionEvidence(*selectedBefore)
	afterEvidence := interactionEvidence(*selectedAfter)
	return Result{
		AssertionID: assertion.ID,
		Type:        assertion.Type,
		Status:      ResultStatusPassed,
		Message:     fmt.Sprintf("ordering assertion %q passed: %s appears before %s", assertion.ID, selectedBefore.InteractionID, selectedAfter.InteractionID),
		Evidence: Evidence{
			Before: &beforeEvidence,
			After:  &afterEvidence,
			Count:  len(beforeMatches) + len(afterMatches),
		},
	}
}

func evaluatePayloadField(run recorder.Run, assertion Assertion) Result {
	matches := matchingInteractions(run.Interactions, assertion.Match)
	violations := make([]InteractionEvidence, 0)
	var firstActual any
	foundAny := false

	for _, interaction := range matches {
		actual, found := valueAtPath(interaction, assertion.Expect.Path)
		if !foundAny {
			firstActual = actual
			foundAny = true
		}
		if !payloadExpectationPasses(assertion.Expect, actual, found) {
			violations = append(violations, interactionEvidence(interaction))
		}
	}

	if len(matches) == 0 {
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusFailed,
			Message:     fmt.Sprintf("payload-field assertion %q failed: no interactions matched selector", assertion.ID),
			Evidence: Evidence{
				Path: assertion.Expect.Path,
			},
		}
	}

	status := ResultStatusFailed
	message := fmt.Sprintf("payload-field assertion %q failed: %d of %d matched interactions violated %s", assertion.ID, len(violations), len(matches), assertion.Expect.Path)
	if len(violations) == 0 {
		status = ResultStatusPassed
		message = fmt.Sprintf("payload-field assertion %q passed for %d matched interactions", assertion.ID, len(matches))
	}

	return Result{
		AssertionID: assertion.ID,
		Type:        assertion.Type,
		Status:      status,
		Message:     message,
		Evidence: Evidence{
			MatchedInteractions: interactionEvidenceList(matches),
			Violations:          violations,
			Count:               len(matches),
			Expected:            payloadExpected(assertion.Expect),
			Actual:              firstActual,
			Path:                assertion.Expect.Path,
		},
	}
}

func evaluateForbiddenOperation(run recorder.Run, assertion Assertion) Result {
	matches := matchingInteractions(run.Interactions, assertion.Match)
	if len(matches) > 0 {
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusFailed,
			Message:     fmt.Sprintf("forbidden-operation assertion %q failed: found %d forbidden interactions", assertion.ID, len(matches)),
			Evidence: Evidence{
				MatchedInteractions: interactionEvidenceList(matches),
				Violations:          interactionEvidenceList(matches),
				Count:               len(matches),
			},
		}
	}

	return Result{
		AssertionID: assertion.ID,
		Type:        assertion.Type,
		Status:      ResultStatusPassed,
		Message:     fmt.Sprintf("forbidden-operation assertion %q passed: no forbidden interactions found", assertion.ID),
		Evidence: Evidence{
			Count: 0,
		},
	}
}

func evaluateFallbackProhibition(run recorder.Run, assertion Assertion) Result {
	matches := matchingInteractions(run.Interactions, assertion.Match)
	disallowed := map[string]bool{}
	for _, tier := range assertion.Expect.DisallowedTiers {
		disallowed[strings.TrimSpace(tier)] = true
	}

	violations := make([]InteractionEvidence, 0)
	for _, interaction := range matches {
		if disallowed[string(interaction.FallbackTier)] {
			violations = append(violations, interactionEvidence(interaction))
		}
	}

	if len(violations) > 0 {
		return Result{
			AssertionID: assertion.ID,
			Type:        assertion.Type,
			Status:      ResultStatusFailed,
			Message:     fmt.Sprintf("fallback-prohibition assertion %q failed: found %d disallowed fallback usages", assertion.ID, len(violations)),
			Evidence: Evidence{
				MatchedInteractions: interactionEvidenceList(matches),
				Violations:          violations,
				Count:               len(matches),
				DisallowedTiers:     append([]string(nil), assertion.Expect.DisallowedTiers...),
			},
		}
	}

	return Result{
		AssertionID: assertion.ID,
		Type:        assertion.Type,
		Status:      ResultStatusPassed,
		Message:     fmt.Sprintf("fallback-prohibition assertion %q passed for %d matched interactions", assertion.ID, len(matches)),
		Evidence: Evidence{
			MatchedInteractions: interactionEvidenceList(matches),
			Count:               len(matches),
			DisallowedTiers:     append([]string(nil), assertion.Expect.DisallowedTiers...),
		},
	}
}

func matchingInteractions(interactions []recorder.Interaction, match Match) []recorder.Interaction {
	matches := make([]recorder.Interaction, 0, len(interactions))
	for _, interaction := range interactions {
		if !interactionMatches(interaction, match) {
			continue
		}
		matches = append(matches, interaction)
	}
	return matches
}

func interactionMatches(interaction recorder.Interaction, match Match) bool {
	if match.Service != "" && interaction.Service != match.Service {
		return false
	}
	if match.Operation != "" && interaction.Operation != match.Operation {
		return false
	}
	if match.InteractionID != "" && interaction.InteractionID != match.InteractionID {
		return false
	}
	if match.FallbackTier != "" && string(interaction.FallbackTier) != match.FallbackTier {
		return false
	}
	if match.EventType != "" && !interactionHasEventType(interaction, match.EventType) {
		return false
	}
	return true
}

func interactionHasEventType(interaction recorder.Interaction, eventType string) bool {
	for _, event := range interaction.Events {
		if string(event.Type) == eventType {
			return true
		}
	}
	return false
}

func interactionEvidenceList(interactions []recorder.Interaction) []InteractionEvidence {
	evidence := make([]InteractionEvidence, 0, len(interactions))
	for _, interaction := range interactions {
		evidence = append(evidence, interactionEvidence(interaction))
	}
	return evidence
}

func interactionEvidence(interaction recorder.Interaction) InteractionEvidence {
	return InteractionEvidence{
		InteractionID: interaction.InteractionID,
		Sequence:      interaction.Sequence,
		Service:       interaction.Service,
		Operation:     interaction.Operation,
		FallbackTier:  string(interaction.FallbackTier),
	}
}

func entityMatches(interactions []recorder.Interaction, ref EntityRef) []EntityEvidence {
	entities := make([]EntityEvidence, 0)
	for _, interaction := range interactions {
		if interaction.Service != ref.Service || interaction.Operation != ref.Operation {
			continue
		}
		value, found := valueAtPath(interaction, ref.Path)
		entity := EntityEvidence{
			Interaction: interactionEvidence(interaction),
			Path:        ref.Path,
			Value:       value,
			Found:       found,
		}
		if !found {
			entities = append(entities, entity)
			continue
		}
		for _, scalar := range scalarEntityValues(value) {
			entities = append(entities, EntityEvidence{
				Interaction: entity.Interaction,
				Path:        ref.Path,
				Value:       scalar,
				Found:       true,
			})
		}
	}
	return entities
}

func equalEntityLinks(leftEntities []EntityEvidence, rightEntities []EntityEvidence) []EntityLinkEvidence {
	links := make([]EntityLinkEvidence, 0)
	for _, left := range leftEntities {
		if !left.Found {
			continue
		}
		for _, right := range rightEntities {
			if !right.Found {
				continue
			}
			if valuesEqual(left.Value, right.Value) {
				links = append(links, EntityLinkEvidence{Left: left, Right: right})
			}
		}
	}
	return links
}

func scalarEntityValues(value any) []any {
	value = normalizeValue(value)
	switch typed := value.(type) {
	case nil:
		return []any{nil}
	case string, bool, float64:
		return []any{typed}
	case []any:
		values := make([]any, 0)
		for _, item := range typed {
			values = append(values, scalarEntityValues(item)...)
		}
		return values
	case map[string]any:
		values := make([]any, 0)
		for _, item := range typed {
			values = append(values, scalarEntityValues(item)...)
		}
		return values
	default:
		return []any{typed}
	}
}

func payloadExpectationPasses(expect Expect, actual any, found bool) bool {
	if expect.Exists != nil {
		return found == *expect.Exists
	}
	if !found {
		return false
	}
	if expect.Equals != nil {
		return valuesEqual(actual, expect.Equals)
	}
	if expect.NotEquals != nil {
		return !valuesEqual(actual, expect.NotEquals)
	}
	return false
}

func payloadExpected(expect Expect) any {
	if expect.Exists != nil {
		return map[string]any{"exists": *expect.Exists}
	}
	if expect.Equals != nil {
		return map[string]any{"equals": expect.Equals}
	}
	if expect.NotEquals != nil {
		return map[string]any{"not_equals": expect.NotEquals}
	}
	return nil
}

func valuesEqual(left any, right any) bool {
	left = normalizeValue(left)
	right = normalizeValue(right)
	return reflect.DeepEqual(left, right)
}

func normalizeValue(value any) any {
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

func valueAtPath(interaction recorder.Interaction, path string) (any, bool) {
	root := map[string]any{
		"interaction_id":        interaction.InteractionID,
		"parent_interaction_id": interaction.ParentInteractionID,
		"sequence":              interaction.Sequence,
		"service":               interaction.Service,
		"operation":             interaction.Operation,
		"protocol":              interaction.Protocol,
		"streaming":             interaction.Streaming,
		"fallback_tier":         interaction.FallbackTier,
		"request":               interaction.Request,
		"events":                interaction.Events,
		"response":              terminalEventData(interaction),
		"scrub_report":          interaction.ScrubReport,
		"latency_ms":            interaction.LatencyMS,
	}
	return lookupPath(root, path)
}

func terminalEventData(interaction recorder.Interaction) map[string]any {
	for idx := len(interaction.Events) - 1; idx >= 0; idx-- {
		event := interaction.Events[idx]
		switch event.Type {
		case recorder.EventTypeResponseReceived, recorder.EventTypeError, recorder.EventTypeTimeout, recorder.EventTypeStreamEnd:
			return event.Data
		}
	}
	return nil
}

func lookupPath(value any, path string) (any, bool) {
	current := normalizeValue(value)
	for _, segment := range strings.Split(path, ".") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return nil, false
		}

		name, indexes, ok := parsePathSegment(segment)
		if !ok {
			return nil, false
		}
		if name != "" {
			object, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			current, ok = object[name]
			if !ok {
				return nil, false
			}
		}
		for _, index := range indexes {
			items, ok := current.([]any)
			if !ok || index < 0 || index >= len(items) {
				return nil, false
			}
			current = items[index]
		}
	}
	return current, true
}

func parsePathSegment(segment string) (string, []int, bool) {
	nameEnd := strings.Index(segment, "[")
	if nameEnd == -1 {
		return segment, nil, true
	}

	name := segment[:nameEnd]
	remaining := segment[nameEnd:]
	indexes := []int{}
	for remaining != "" {
		if !strings.HasPrefix(remaining, "[") {
			return "", nil, false
		}
		end := strings.Index(remaining, "]")
		if end == -1 {
			return "", nil, false
		}
		index, err := strconv.Atoi(remaining[1:end])
		if err != nil {
			return "", nil, false
		}
		indexes = append(indexes, index)
		remaining = remaining[end+1:]
	}
	return name, indexes, true
}

func AllPassed(results []Result) bool {
	return !slices.ContainsFunc(results, func(result Result) bool {
		return result.Status != ResultStatusPassed
	})
}
