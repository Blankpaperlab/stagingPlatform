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
	if !strings.Contains(file.RestrictedActions[0].ClassifierReason, `financial term "refund"`) {
		t.Fatalf("refund classifier reason = %q, want financial refund reason", file.RestrictedActions[0].ClassifierReason)
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
		"classifier_reason:",
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

func TestClassifyInteractionHTTPAPIRiskRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		operation  string
		method     string
		url        string
		want       SideEffect
		wantReason string
	}{
		{
			name:       "get read",
			operation:  "/v1/customers/search",
			method:     "GET",
			url:        "https://api.example.test/v1/customers/search",
			want:       SideEffectRead,
			wantReason: "HTTP method GET classified as read",
		},
		{
			name:       "head read",
			operation:  "/v1/customers/cus_123",
			method:     "HEAD",
			url:        "https://api.example.test/v1/customers/cus_123",
			want:       SideEffectRead,
			wantReason: "HTTP method HEAD classified as read",
		},
		{
			name:       "operation method read",
			operation:  "GET /v1/customers/search",
			method:     "",
			url:        "https://api.example.test/v1/customers/search",
			want:       SideEffectRead,
			wantReason: "HTTP method GET classified as read",
		},
		{
			name:       "post write",
			operation:  "/v1/customers",
			method:     "POST",
			url:        "https://api.example.test/v1/customers",
			want:       SideEffectWrite,
			wantReason: "HTTP method POST classified as write",
		},
		{
			name:       "put write",
			operation:  "/v1/customers/cus_123",
			method:     "PUT",
			url:        "https://api.example.test/v1/customers/cus_123",
			want:       SideEffectWrite,
			wantReason: "HTTP method PUT classified as write",
		},
		{
			name:       "patch write",
			operation:  "/v1/customers/cus_123",
			method:     "PATCH",
			url:        "https://api.example.test/v1/customers/cus_123",
			want:       SideEffectWrite,
			wantReason: "HTTP method PATCH classified as write",
		},
		{
			name:       "delete destructive",
			operation:  "/v1/customers/cus_123",
			method:     "DELETE",
			url:        "https://api.example.test/v1/customers/cus_123",
			want:       SideEffectDestructive,
			wantReason: "HTTP method DELETE classified as destructive",
		},
		{
			name:       "financial endpoint",
			operation:  "POST /v1/refunds",
			method:     "POST",
			url:        "https://api.example.test/v1/refunds",
			want:       SideEffectFinancial,
			wantReason: `financial term "refund"`,
		},
		{
			name:       "external message endpoint",
			operation:  "POST /chat.postMessage",
			method:     "POST",
			url:        "https://slack.example.test/chat.postMessage",
			want:       SideEffectExternalMessage,
			wantReason: `external-message term "message"`,
		},
		{
			name:       "destructive endpoint",
			operation:  "POST /v1/archive",
			method:     "POST",
			url:        "https://api.example.test/v1/archive",
			want:       SideEffectDestructive,
			wantReason: `destructive term "archive"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			classification := ClassifyInteraction(contractInteraction("run_classify", 1, "internal-api", tt.operation, recorder.ProtocolHTTPS, tt.method, tt.url, nil))
			if classification.SideEffect != tt.want {
				t.Fatalf("SideEffect = %q, want %q; reason=%q", classification.SideEffect, tt.want, classification.Reason)
			}
			if !strings.Contains(classification.Reason, tt.wantReason) {
				t.Fatalf("Reason = %q, want containing %q", classification.Reason, tt.wantReason)
			}
		})
	}
}

func TestClassifyInteractionToolRiskRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tool       string
		body       any
		events     []recorder.Event
		want       SideEffect
		wantReason string
	}{
		{
			name:       "declared request metadata",
			tool:       "lookup_customer",
			body:       map[string]any{"side_effect": "read"},
			want:       SideEffectRead,
			wantReason: "tool request body declares side_effect=read",
		},
		{
			name:       "declared event metadata",
			tool:       "lookup_customer",
			events:     []recorder.Event{{Sequence: 1, Type: recorder.EventTypeToolCallStart, Data: map[string]any{"side_effect": "read"}}},
			want:       SideEffectRead,
			wantReason: "tool event declares side_effect=read",
		},
		{
			name:       "missing metadata unknown",
			tool:       "normalize_customer",
			want:       SideEffectUnknown,
			wantReason: "tool has no side_effect metadata and name did not match known risk rules",
		},
		{
			name:       "send email external message",
			tool:       "send_email",
			want:       SideEffectExternalMessage,
			wantReason: `external-message term "email"`,
		},
		{
			name:       "refund customer financial",
			tool:       "refund_customer",
			want:       SideEffectFinancial,
			wantReason: `financial term "refund"`,
		},
		{
			name:       "delete user destructive",
			tool:       "delete_user",
			want:       SideEffectDestructive,
			wantReason: `destructive term "delete"`,
		},
		{
			name:       "update ticket write",
			tool:       "update_ticket",
			want:       SideEffectWrite,
			wantReason: `write term "update"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interaction := contractInteraction("run_tool_classify", 1, "stagehand.tool", tt.tool, recorder.ProtocolTool, "CALL", "stagehand://tool/"+tt.tool, tt.body)
			if tt.events != nil {
				interaction.Events = tt.events
			}
			classification := ClassifyInteraction(interaction)
			if classification.SideEffect != tt.want {
				t.Fatalf("SideEffect = %q, want %q; reason=%q", classification.SideEffect, tt.want, classification.Reason)
			}
			if !strings.Contains(classification.Reason, tt.wantReason) {
				t.Fatalf("Reason = %q, want containing %q", classification.Reason, tt.wantReason)
			}
		})
	}
}

func TestClassifyInteractionToolConfigOverride(t *testing.T) {
	t.Parallel()

	interaction := contractInteraction("run_tool_override", 1, "stagehand.tool", "normalize_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/normalize_customer", nil)
	classification := ClassifyInteractionWithOverrides(interaction, []ClassificationOverride{
		{
			Tool:       "normalize_customer",
			SideEffect: SideEffectWrite,
			Reason:     "config override for normalization writes",
		},
	})

	if classification.SideEffect != SideEffectWrite {
		t.Fatalf("SideEffect = %q, want write", classification.SideEffect)
	}
	if classification.Reason != "config override for normalization writes" {
		t.Fatalf("Reason = %q, want config override reason", classification.Reason)
	}
}

func TestGenerateFromRunAppliesToolClassificationOverride(t *testing.T) {
	t.Parallel()

	run := recorder.Run{
		RunID:       "run_tool_override",
		SessionName: "tool-agent",
		Interactions: []recorder.Interaction{
			contractInteraction("run_tool_override", 1, "stagehand.tool", "normalize_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/normalize_customer", nil),
		},
	}
	file, summary := GenerateFromRun(run, GenerateOptions{
		ClassificationOverrides: []ClassificationOverride{
			{Tool: "normalize_customer", SideEffect: SideEffectWrite, Reason: "configured as a write"},
		},
	})

	if summary.RestrictedActions != 1 || len(file.RestrictedActions) != 1 {
		t.Fatalf("summary=%#v restricted=%#v, want one restricted override action", summary, file.RestrictedActions)
	}
	action := file.RestrictedActions[0]
	if action.SideEffect != SideEffectWrite || action.ClassifierReason != "configured as a write" {
		t.Fatalf("action = %#v, want write override reason", action)
	}
}

func TestClassifyInteractionDatabaseRiskRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		operation  string
		body       any
		want       SideEffect
		wantReason string
	}{
		{
			name:       "select read",
			operation:  "SELECT * FROM customers WHERE email = 'user_d3aee2997c9f@scrub.local'",
			want:       SideEffectRead,
			wantReason: "database operation SELECT classified as read",
		},
		{
			name:       "insert write",
			operation:  "INSERT INTO tickets(id) VALUES ($1)",
			want:       SideEffectWrite,
			wantReason: "database operation INSERT classified as write",
		},
		{
			name:       "update write",
			operation:  "UPDATE tickets SET status = 'open'",
			want:       SideEffectWrite,
			wantReason: "database operation UPDATE classified as write",
		},
		{
			name:       "upsert write",
			operation:  "UPSERT INTO tickets VALUES ($1)",
			want:       SideEffectWrite,
			wantReason: "database operation UPSERT classified as write",
		},
		{
			name:       "delete destructive",
			operation:  "DELETE FROM users WHERE id = $1",
			want:       SideEffectDestructive,
			wantReason: "database operation DELETE classified as destructive",
		},
		{
			name:       "drop destructive",
			operation:  "DROP TABLE users",
			want:       SideEffectDestructive,
			wantReason: "database operation DROP classified as destructive",
		},
		{
			name:       "truncate destructive",
			operation:  "TRUNCATE TABLE sessions",
			want:       SideEffectDestructive,
			wantReason: "database operation TRUNCATE classified as destructive",
		},
		{
			name:       "alter destructive",
			operation:  "ALTER TABLE users ADD COLUMN disabled_at timestamptz",
			want:       SideEffectDestructive,
			wantReason: "database operation ALTER classified as destructive",
		},
		{
			name:       "body sql destructive",
			operation:  "db.query",
			body:       map[string]any{"sql": "DROP TABLE users"},
			want:       SideEffectDestructive,
			wantReason: "database operation DROP classified as destructive",
		},
		{
			name:       "commented query read",
			operation:  "-- generated\nSELECT id FROM users",
			want:       SideEffectRead,
			wantReason: "database operation SELECT classified as read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interaction := contractInteraction("run_db_classify", 1, "postgres", tt.operation, recorder.ProtocolPostgres, "", "", tt.body)
			classification := ClassifyInteraction(interaction)
			if classification.SideEffect != tt.want {
				t.Fatalf("SideEffect = %q, want %q; reason=%q", classification.SideEffect, tt.want, classification.Reason)
			}
			if !strings.Contains(classification.Reason, tt.wantReason) {
				t.Fatalf("Reason = %q, want containing %q", classification.Reason, tt.wantReason)
			}
			if !strings.Contains(classification.Reason, "scrubbed query snippet") {
				t.Fatalf("Reason = %q, want scrubbed query snippet", classification.Reason)
			}
		})
	}
}

func TestClassifyInteractionDatabaseSnippetRequiresScrubReport(t *testing.T) {
	t.Parallel()

	interaction := contractInteraction("run_db_unscrubbed", 1, "postgres", "SELECT * FROM users WHERE email = 'secret@example.com'", recorder.ProtocolPostgres, "", "", nil)
	interaction.ScrubReport = recorder.ScrubReport{}

	classification := ClassifyInteraction(interaction)
	if classification.SideEffect != SideEffectRead {
		t.Fatalf("SideEffect = %q, want read", classification.SideEffect)
	}
	if strings.Contains(classification.Reason, "secret@example.com") || strings.Contains(classification.Reason, "query snippet") {
		t.Fatalf("Reason = %q, want no raw query snippet without scrub report", classification.Reason)
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
