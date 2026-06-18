package gates

import (
	"strings"
	"testing"
	"time"

	analysisassertions "stagehand/internal/analysis/assertions"
	"stagehand/internal/recorder"
)

func TestEvaluateReleaseGatesViaAssertions(t *testing.T) {
	t.Parallel()

	base := gateRun("run_base", []recorder.Interaction{
		gateToolInteraction("run_base", "int_base_lookup", 1, "lookup_customer", "read", nil, nil),
		gateHTTPInteraction("run_base", "int_base_refund", 2, "stripe", "POST /v1/refunds", "POST", "https://api.stripe.com/v1/refunds", map[string]any{
			"amount":      2500,
			"approval_id": "approval_123",
		}),
	})
	candidate := gateRun("run_candidate", []recorder.Interaction{
		gateToolInteraction("run_candidate", "int_lookup", 1, "lookup_customer", "read", nil, nil),
		gateHTTPInteraction("run_candidate", "int_refund", 2, "stripe", "POST /v1/refunds", "POST", "https://api.stripe.com/v1/refunds", map[string]any{
			"amount": 7500,
		}),
		gateToolInteraction("run_candidate", "int_email", 3, "send_email", "external_message", map[string]any{
			"destination_type": "external",
			"channel":          "push",
			"domain":           "customer.example",
		}, nil),
		gateHTTPInteraction("run_candidate", "int_payout", 4, "stripe", "POST /v1/payouts", "POST", "https://api.stripe.com/v1/payouts", map[string]any{
			"amount": 1000,
		}),
	})
	file := File{
		SchemaVersion: SchemaVersion,
		ReleaseGates: []Gate{
			{
				Name: "Refunds over $50 require approval",
				BlockIf: &BlockIf{
					ActionSelector:  ActionSelector{Service: "stripe", Operation: "POST /v1/refunds"},
					AmountGT:        floatPtr(5000),
					ApprovalMissing: true,
				},
			},
			{
				Name:         "Customer lookup must happen before billing action",
				RequireOrder: &RequireOrder{Before: ActionSelector{Tool: "lookup_customer"}, After: ActionSelector{Service: "stripe", Operation: "POST /v1/refunds"}},
			},
			{
				Name:            "Allowed customer message channels",
				AllowedChannels: &AllowedValues{ActionSelector: ActionSelector{SideEffect: SideEffectExternalMessage}, Values: []string{"email", "sms"}},
			},
			{
				Name:            "No new financial actions",
				ForbidNewAction: &ActionSelector{SideEffect: SideEffectFinancial},
			},
		},
	}

	result, err := Evaluate(candidate, file, EvaluationOptions{BaseRun: &base})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Status != analysisassertions.ResultStatusFailed {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if result.Summary.Total != 4 || result.Summary.Passed != 1 || result.Summary.Failed != 3 {
		t.Fatalf("Summary = %#v, want 1 passed and 3 failed", result.Summary)
	}
	if len(result.Assertions.Assertions) != 4 {
		t.Fatalf("translated assertion count = %d, want 4", len(result.Assertions.Assertions))
	}

	refund := gateResultByName(t, result.Results, "Refunds over $50 require approval")
	if refund.AssertionID != "gate-001" || strings.Contains(refund.Message, "gate-001") {
		t.Fatalf("refund gate result = %#v, want gate name message and backing assertion id", refund)
	}
	if len(refund.Evidence.MatchedInteractions) != 1 || refund.Evidence.MatchedInteractions[0].InteractionID != "int_refund" {
		t.Fatalf("refund evidence = %#v, want int_refund", refund.Evidence.MatchedInteractions)
	}

	order := gateResultByName(t, result.Results, "Customer lookup must happen before billing action")
	if order.Status != analysisassertions.ResultStatusPassed || order.Evidence.Before == nil || order.Evidence.After == nil {
		t.Fatalf("order gate = %#v, want passed with before/after evidence", order)
	}

	channel := gateResultByName(t, result.Results, "Allowed customer message channels")
	if channel.Status != analysisassertions.ResultStatusFailed || channel.Evidence.MatchedInteractions[0].InteractionID != "int_email" {
		t.Fatalf("channel gate = %#v, want int_email violation", channel)
	}

	newAction := gateResultByName(t, result.Results, "No new financial actions")
	if newAction.Status != analysisassertions.ResultStatusFailed || newAction.Evidence.MatchedInteractions[0].InteractionID != "int_payout" {
		t.Fatalf("new-action gate = %#v, want new payout evidence", newAction)
	}
}

func TestEvaluateForbidUnknownRiskGate(t *testing.T) {
	t.Parallel()

	candidate := gateRun("run_candidate", []recorder.Interaction{
		gateToolInteraction("run_candidate", "int_custom", 1, "frobnicate", "", nil, nil),
	})
	file := File{
		SchemaVersion: SchemaVersion,
		ReleaseGates: []Gate{
			{
				Name:              "Unknown risk blocks release",
				ForbidUnknownRisk: &ForbidUnknownRisk{},
			},
		},
	}

	result, err := Evaluate(candidate, file, EvaluationOptions{})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	got := gateResultByName(t, result.Results, "Unknown risk blocks release")
	if got.Status != analysisassertions.ResultStatusFailed || got.Evidence.Count != 1 {
		t.Fatalf("unknown-risk gate = %#v, want one failure", got)
	}
}

func TestEvaluateAllowedChannelsAndDomainsReadNestedToolArguments(t *testing.T) {
	t.Parallel()

	candidate := gateRun("run_candidate", []recorder.Interaction{
		gateToolInteraction("run_candidate", "int_email", 1, "send_email", "external_message", map[string]any{
			"channel":          "email",
			"destination_type": "external",
			"domain":           "customer.example",
		}, nil),
	})
	file := File{
		SchemaVersion: SchemaVersion,
		ReleaseGates: []Gate{
			{
				Name:            "Allowed customer message channels",
				AllowedChannels: &AllowedValues{ActionSelector: ActionSelector{SideEffect: SideEffectExternalMessage}, Values: []string{"email", "sms"}},
			},
			{
				Name:           "Allowed customer message domains",
				AllowedDomains: &AllowedValues{ActionSelector: ActionSelector{SideEffect: SideEffectExternalMessage}, Values: []string{"customer.example"}},
			},
		},
	}

	result, err := Evaluate(candidate, file, EvaluationOptions{})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Status != analysisassertions.ResultStatusPassed {
		t.Fatalf("Status = %q, want passed: %#v", result.Status, result.Results)
	}
	for _, gate := range result.Results {
		if gate.Evidence.Count != 0 {
			t.Fatalf("gate %q evidence count = %d, want no allowed-list violations", gate.GateName, gate.Evidence.Count)
		}
	}
}

func TestEvaluateBlockIfDestinationTypeReadsNestedToolArguments(t *testing.T) {
	t.Parallel()

	candidate := gateRun("run_candidate", []recorder.Interaction{
		gateToolInteraction("run_candidate", "int_external_message", 1, "send_email", "external_message", map[string]any{
			"channel":          "email",
			"destination_type": "external",
			"domain":           "customer.example",
		}, nil),
	})
	file := File{
		SchemaVersion: SchemaVersion,
		ReleaseGates: []Gate{
			{
				Name: "No external messages",
				BlockIf: &BlockIf{
					ActionSelector: ActionSelector{
						SideEffect:      SideEffectExternalMessage,
						DestinationType: "external",
					},
				},
			},
		},
	}

	result, err := Evaluate(candidate, file, EvaluationOptions{})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	got := gateResultByName(t, result.Results, "No external messages")
	if got.Status != analysisassertions.ResultStatusFailed {
		t.Fatalf("Status = %q, want failed: %#v", got.Status, got)
	}
	if len(got.Evidence.MatchedInteractions) != 1 || got.Evidence.MatchedInteractions[0].InteractionID != "int_external_message" {
		t.Fatalf("evidence = %#v, want nested destination_type tool interaction", got.Evidence.MatchedInteractions)
	}
}

func gateResultByName(t *testing.T, results []Result, name string) Result {
	t.Helper()

	for _, result := range results {
		if result.GateName == name {
			return result
		}
	}
	t.Fatalf("missing gate result %q in %#v", name, results)
	return Result{}
}

func gateRun(runID string, interactions []recorder.Interaction) recorder.Run {
	now := time.Date(2026, time.June, 12, 12, 0, 0, 0, time.UTC)
	return recorder.Run{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              runID,
		SessionName:        "gate-agent",
		Mode:               recorder.RunModeReplay,
		Status:             recorder.RunStatusComplete,
		StartedAt:          now,
		EndedAt:            &now,
		Interactions:       interactions,
	}
}

func gateHTTPInteraction(runID, interactionID string, sequence int, service, operation, method, rawURL string, body map[string]any) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       service,
		Operation:     operation,
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			Method: method,
			URL:    rawURL,
			Body:   body,
		},
		Events: []recorder.Event{
			{Sequence: 1, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, Type: recorder.EventTypeResponseReceived, Data: map[string]any{"status": 200}},
		},
		ScrubReport: recorder.ScrubReport{ScrubPolicyVersion: "v1", SessionSaltID: "salt_gates"},
	}
}

func gateToolInteraction(runID, interactionID string, sequence int, name, sideEffect string, args map[string]any, result map[string]any) recorder.Interaction {
	body := map[string]any{"name": name, "arguments": args}
	if sideEffect != "" {
		body["side_effect"] = sideEffect
	}
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       "stagehand.tool",
		Operation:     name,
		Protocol:      recorder.ProtocolTool,
		Request: recorder.Request{
			Method: "CALL",
			URL:    "stagehand://tool/" + name,
			Body:   body,
		},
		Events: []recorder.Event{
			{Sequence: 1, Type: recorder.EventTypeToolCallStart, Data: body},
			{Sequence: 2, Type: recorder.EventTypeToolCallEnd, Data: map[string]any{"result": result}},
		},
		ScrubReport: recorder.ScrubReport{ScrubPolicyVersion: "v1", SessionSaltID: "salt_gates"},
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
