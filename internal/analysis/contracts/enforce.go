package contracts

import (
	"fmt"
	"strings"

	"stagehand/internal/recorder"
)

type ViolationType string

const (
	ViolationNewUnapprovedAction          ViolationType = "new_unapproved_action"
	ViolationForbiddenAction              ViolationType = "forbidden_action"
	ViolationRestrictedActionApprovalMiss ViolationType = "restricted_action_approval_missing"
)

type EvaluationStatus string

const (
	EvaluationStatusPassed EvaluationStatus = "passed"
	EvaluationStatusFailed EvaluationStatus = "failed"
)

type EvaluationSummary struct {
	Total      int `json:"total"`
	Forbidden  int `json:"forbidden"`
	Restricted int `json:"restricted_without_approval"`
	Unapproved int `json:"new_unapproved"`
}

type EvaluationResult struct {
	Status     EvaluationStatus  `json:"status"`
	Summary    EvaluationSummary `json:"summary"`
	Violations []Violation       `json:"violations,omitempty"`
}

type Violation struct {
	Type       ViolationType         `json:"type"`
	Service    string                `json:"service,omitempty"`
	Operation  string                `json:"operation,omitempty"`
	Tool       string                `json:"tool,omitempty"`
	SideEffect SideEffect            `json:"side_effect,omitempty"`
	Reason     string                `json:"reason,omitempty"`
	Evidence   []InteractionEvidence `json:"evidence"`
}

type InteractionEvidence struct {
	RunID         string `json:"run_id"`
	InteractionID string `json:"interaction_id"`
	Sequence      int    `json:"sequence"`
	Service       string `json:"service,omitempty"`
	Operation     string `json:"operation,omitempty"`
	Tool          string `json:"tool,omitempty"`
	RequestMethod string `json:"request_method,omitempty"`
	RequestURL    string `json:"request_url,omitempty"`
	FallbackTier  string `json:"fallback_tier,omitempty"`
}

func Evaluate(run recorder.Run, file File) (EvaluationResult, error) {
	if err := file.Validate(); err != nil {
		return EvaluationResult{}, err
	}

	allowed := indexActions(file.AllowedActions)
	restricted := indexActions(file.RestrictedActions)
	forbidden := indexActions(file.ForbiddenActions)

	violationsByKey := map[string]*Violation{}
	orderedKeys := []string{}
	for _, interaction := range run.Interactions {
		selector, ok := selectorFromInteraction(interaction)
		if !ok {
			continue
		}
		key := selectorKey(selector)
		evidence := evidenceFromInteraction(interaction, selector)

		if action, found := forbidden[key]; found {
			addViolation(violationsByKey, &orderedKeys, violationKey(ViolationForbiddenAction, key), Violation{
				Type:       ViolationForbiddenAction,
				Service:    selector.Service,
				Operation:  selector.Operation,
				Tool:       selector.Tool,
				SideEffect: action.SideEffect,
				Reason:     strings.TrimSpace(action.Reason),
				Evidence:   []InteractionEvidence{evidence},
			})
			continue
		}

		if action, found := restricted[key]; found {
			if action.RequiresApproval && !interactionHasApproval(interaction, action) {
				addViolation(violationsByKey, &orderedKeys, violationKey(ViolationRestrictedActionApprovalMiss, key), Violation{
					Type:       ViolationRestrictedActionApprovalMiss,
					Service:    selector.Service,
					Operation:  selector.Operation,
					Tool:       selector.Tool,
					SideEffect: action.SideEffect,
					Reason:     "restricted action requires approval but no approval evidence was captured",
					Evidence:   []InteractionEvidence{evidence},
				})
			}
			continue
		}

		if _, found := allowed[key]; found {
			continue
		}

		sideEffect := classifyInteraction(interaction)
		addViolation(violationsByKey, &orderedKeys, violationKey(ViolationNewUnapprovedAction, key), Violation{
			Type:       ViolationNewUnapprovedAction,
			Service:    selector.Service,
			Operation:  selector.Operation,
			Tool:       selector.Tool,
			SideEffect: sideEffect,
			Reason:     "action is not present in allowed_actions, restricted_actions, or forbidden_actions",
			Evidence:   []InteractionEvidence{evidence},
		})
	}

	result := EvaluationResult{
		Status: EvaluationStatusPassed,
	}
	for _, key := range orderedKeys {
		violation := *violationsByKey[key]
		result.Violations = append(result.Violations, violation)
		result.Summary.Total++
		switch violation.Type {
		case ViolationForbiddenAction:
			result.Summary.Forbidden++
		case ViolationRestrictedActionApprovalMiss:
			result.Summary.Restricted++
		case ViolationNewUnapprovedAction:
			result.Summary.Unapproved++
		}
	}
	if result.Summary.Total > 0 {
		result.Status = EvaluationStatusFailed
	}
	return result, nil
}

func indexActions(actions []Action) map[string]Action {
	index := map[string]Action{}
	for _, action := range actions {
		key := selectorKey(ActionSelector{
			Service:   action.Service,
			Operation: action.Operation,
			Tool:      action.Tool,
		})
		index[key] = action
	}
	return index
}

func addViolation(violations map[string]*Violation, order *[]string, key string, violation Violation) {
	existing := violations[key]
	if existing == nil {
		copy := violation
		violations[key] = &copy
		*order = append(*order, key)
		return
	}
	existing.Evidence = append(existing.Evidence, violation.Evidence...)
}

func violationKey(violationType ViolationType, selectorKey string) string {
	return string(violationType) + "\n" + selectorKey
}

func evidenceFromInteraction(interaction recorder.Interaction, selector ActionSelector) InteractionEvidence {
	return InteractionEvidence{
		RunID:         interaction.RunID,
		InteractionID: interaction.InteractionID,
		Sequence:      interaction.Sequence,
		Service:       selector.Service,
		Operation:     selector.Operation,
		Tool:          selector.Tool,
		RequestMethod: interaction.Request.Method,
		RequestURL:    interaction.Request.URL,
		FallbackTier:  string(interaction.FallbackTier),
	}
}

func interactionHasApproval(interaction recorder.Interaction, action Action) bool {
	if action.Approval != nil && strings.TrimSpace(action.Approval.EvidencePath) != "" {
		if valueAtPath(interactionPathRoot(interaction), action.Approval.EvidencePath) != nil {
			return true
		}
		// Also accept concise paths that start at the request body or event data.
		for _, event := range interaction.Events {
			if valueAtPath(event.Data, action.Approval.EvidencePath) != nil {
				return true
			}
		}
	}
	if HasApprovalEvidence(interaction.Request.Body) {
		return true
	}
	for _, event := range interaction.Events {
		if HasApprovalEvidence(event.Data) {
			return true
		}
	}
	return false
}

func interactionPathRoot(interaction recorder.Interaction) map[string]any {
	events := make([]any, 0, len(interaction.Events))
	for _, event := range interaction.Events {
		events = append(events, map[string]any{
			"sequence": event.Sequence,
			"type":     event.Type,
			"data":     event.Data,
		})
	}
	return map[string]any{
		"interaction_id": interaction.InteractionID,
		"sequence":       interaction.Sequence,
		"service":        interaction.Service,
		"operation":      interaction.Operation,
		"request": map[string]any{
			"url":     interaction.Request.URL,
			"method":  interaction.Request.Method,
			"headers": interaction.Request.Headers,
			"body":    interaction.Request.Body,
		},
		"events": events,
	}
}

func valueAtPath(value any, path string) any {
	current := value
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil
		}
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		default:
			return nil
		}
	}
	return current
}

func HasApprovalEvidence(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isApprovalKey(key) && truthy(item) {
				return true
			}
			if HasApprovalEvidence(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if HasApprovalEvidence(item) {
				return true
			}
		}
	}
	return false
}

func isApprovalKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "approval", "approved", "approval_id", "approval_token", "human_approval", "approved_by":
		return true
	default:
		return false
	}
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed != "" && !strings.EqualFold(trimmed, "false")
	case int, int64, float64, float32:
		return fmt.Sprint(typed) != "0"
	default:
		return true
	}
}
