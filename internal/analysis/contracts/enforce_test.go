package contracts

import (
	"testing"
	"time"

	"stagehand/internal/recorder"
)

func TestEvaluateFailsOnNewForbiddenAndMissingApproval(t *testing.T) {
	t.Parallel()

	run := recorder.Run{
		RunID:       "run_candidate",
		SessionName: "refund-agent",
		StartedAt:   time.Now().UTC(),
		Interactions: []recorder.Interaction{
			contractInteraction("run_candidate", 1, "openai", "chat.completions.create", recorder.ProtocolHTTPS, "POST", "https://api.openai.com/v1/chat/completions", map[string]any{
				"model": "gpt-5.4",
			}),
			contractInteraction("run_candidate", 2, "stripe", "POST /v1/refunds", recorder.ProtocolHTTPS, "POST", "https://api.stripe.com/v1/refunds", map[string]any{
				"amount": 5000,
			}),
			contractInteraction("run_candidate", 3, "postgres", "DELETE", recorder.ProtocolPostgres, "", "", nil),
			contractInteraction("run_candidate", 4, "slack", "POST /chat.postMessage", recorder.ProtocolHTTPS, "POST", "https://slack.example/chat.postMessage", nil),
		},
	}
	file := File{
		SchemaVersion: SchemaVersion,
		Agent:         Agent{Name: "refund-agent"},
		AllowedActions: []Action{
			{Service: "openai", Operation: "chat.completions.create", SideEffect: SideEffectRead},
		},
		RestrictedActions: []Action{
			{Service: "stripe", Operation: "POST /v1/refunds", SideEffect: SideEffectFinancial, RequiresApproval: true},
		},
		ForbiddenActions: []Action{
			{Service: "postgres", Operation: "DELETE", SideEffect: SideEffectDestructive, Reason: "destructive database write"},
		},
	}

	result, err := Evaluate(run, file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Status != EvaluationStatusFailed {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if result.Summary.Total != 3 || result.Summary.Forbidden != 1 || result.Summary.Restricted != 1 || result.Summary.Unapproved != 1 {
		t.Fatalf("Summary = %#v, want one of each violation", result.Summary)
	}
	if result.Violations[0].Type != ViolationRestrictedActionApprovalMiss || result.Violations[0].Evidence[0].InteractionID != "int_contract_2" {
		t.Fatalf("first violation = %#v, want restricted approval evidence for interaction 2", result.Violations[0])
	}
	if result.Violations[1].Type != ViolationForbiddenAction || result.Violations[1].Reason != "destructive database write" {
		t.Fatalf("second violation = %#v, want forbidden database action", result.Violations[1])
	}
	if result.Violations[2].Type != ViolationNewUnapprovedAction || result.Violations[2].Evidence[0].RequestURL != "https://slack.example/chat.postMessage" {
		t.Fatalf("third violation = %#v, want new unapproved slack action evidence", result.Violations[2])
	}
}

func TestEvaluatePassesRestrictedActionWithApprovalEvidence(t *testing.T) {
	t.Parallel()

	run := recorder.Run{
		RunID:       "run_candidate",
		SessionName: "refund-agent",
		StartedAt:   time.Now().UTC(),
		Interactions: []recorder.Interaction{
			contractInteraction("run_candidate", 1, "stripe", "POST /v1/refunds", recorder.ProtocolHTTPS, "POST", "https://api.stripe.com/v1/refunds", map[string]any{
				"amount":      5000,
				"approval_id": "approval_123",
			}),
		},
	}
	file := File{
		SchemaVersion: SchemaVersion,
		Agent:         Agent{Name: "refund-agent"},
		RestrictedActions: []Action{
			{Service: "stripe", Operation: "POST /v1/refunds", SideEffect: SideEffectFinancial, RequiresApproval: true},
		},
	}

	result, err := Evaluate(run, file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Status != EvaluationStatusPassed || result.Summary.Total != 0 {
		t.Fatalf("result = %#v, want passed without violations", result)
	}
}
