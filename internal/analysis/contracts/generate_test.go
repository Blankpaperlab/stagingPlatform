package contracts

import (
	"strings"
	"testing"
	"time"

	"stagehand/internal/recorder"
)

func TestGenerateFromRunClassifiesObservedActions(t *testing.T) {
	t.Parallel()

	run := recorder.Run{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "test",
		RuntimeVersion:     "test",
		ScrubPolicyVersion: "v1",
		RunID:              "run_contract",
		SessionName:        "refund-agent",
		Mode:               recorder.RunModeRecord,
		Status:             recorder.RunStatusComplete,
		StartedAt:          time.Now().UTC(),
		EndedAt:            ptrTime(time.Now().UTC().Add(time.Second)),
		Interactions: []recorder.Interaction{
			contractInteraction("run_contract", 1, "openai", "chat.completions.create", recorder.ProtocolHTTPS, "POST", "https://api.openai.com/v1/chat/completions", map[string]any{
				"model": "gpt-5.4",
			}),
			contractInteraction("run_contract", 2, "stagehand.tool", "lookup_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/lookup_customer", map[string]any{
				"name":        "lookup_customer",
				"side_effect": "read",
			}),
			contractInteraction("run_contract", 3, "stripe", "POST /v1/refunds", recorder.ProtocolHTTPS, "POST", "https://api.stripe.com/v1/refunds", map[string]any{
				"amount": 5000,
			}),
		},
	}

	file, summary := GenerateFromRun(run, GenerateOptions{})
	if err := file.Validate(); err != nil {
		t.Fatalf("generated contract Validate() error = %v", err)
	}
	if file.Agent.Name != "refund-agent" {
		t.Fatalf("Agent.Name = %q, want refund-agent", file.Agent.Name)
	}
	if len(file.Agent.Models) != 1 || file.Agent.Models[0] != "gpt-5.4" {
		t.Fatalf("Agent.Models = %#v, want gpt-5.4", file.Agent.Models)
	}
	if summary.AllowedActions != 2 || summary.RestrictedActions != 1 {
		t.Fatalf("summary = %#v, want 2 allowed and 1 restricted", summary)
	}
	if file.RestrictedActions[0].SideEffect != SideEffectFinancial {
		t.Fatalf("refund side effect = %q, want financial", file.RestrictedActions[0].SideEffect)
	}
	if file.RestrictedActions[0].MaxAmount == nil || *file.RestrictedActions[0].MaxAmount != 5000 {
		t.Fatalf("refund max amount = %#v, want 5000", file.RestrictedActions[0].MaxAmount)
	}
	if !file.RestrictedActions[0].RequiresApproval {
		t.Fatalf("refund requires approval = false, want true")
	}
}

func TestRenderYAMLIncludesReviewCommentsAndParses(t *testing.T) {
	t.Parallel()

	run := recorder.Run{
		RunID:       "run_contract",
		SessionName: "refund-agent",
		Interactions: []recorder.Interaction{
			contractInteraction("run_contract", 1, "openai", "chat.completions.create", recorder.ProtocolHTTPS, "POST", "https://api.openai.com/v1/chat/completions", map[string]any{
				"model": "gpt-5.4",
			}),
			contractInteraction("run_contract", 2, "stagehand.tool", "lookup_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/lookup_customer", map[string]any{
				"name":        "lookup_customer",
				"side_effect": "read",
			}),
			contractInteraction("run_contract", 3, "internal-api", "MYSTERY /v1/side-effect", recorder.ProtocolHTTPS, "CUSTOM", "https://internal.example/v1/side-effect", nil),
			contractInteraction("run_contract", 4, "stripe", "POST /v1/refunds", recorder.ProtocolHTTPS, "POST", "https://api.stripe.com/v1/refunds", map[string]any{
				"amount": 5000,
			}),
		},
	}
	file, _ := GenerateFromRun(run, GenerateOptions{})
	rendered := RenderYAML(file, "run_contract", "base_contract")
	renderedText := string(rendered)
	for _, want := range []string{
		"# Review side_effect",
		"# Service: openai",
		"# Service: stripe",
		"# Tools",
		"# Suggested risk: read",
		"# Suggested risk: financial",
		"# Suggested release gate: require approval",
		"# Suggested risk: unknown",
		"# UNKNOWN RISK:",
	} {
		if !strings.Contains(renderedText, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, renderedText)
		}
	}
	parsed, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse(rendered) error = %v\n%s", err, string(rendered))
	}
	if parsed.AllowedActions[0].Service != "openai" {
		t.Fatalf("parsed allowed action = %#v, want openai", parsed.AllowedActions[0])
	}
}

func contractInteraction(runID string, sequence int, service, operation string, protocol recorder.Protocol, method, rawURL string, body any) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: "int_contract_" + string(rune('0'+sequence)),
		Sequence:      sequence,
		Service:       service,
		Operation:     operation,
		Protocol:      protocol,
		Request: recorder.Request{
			URL:    rawURL,
			Method: method,
			Body:   body,
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: 1, SimTMS: 1, Type: recorder.EventTypeResponseReceived},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fixture",
		},
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
