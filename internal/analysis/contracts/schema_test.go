package contracts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidBehaviorContract(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(validContractYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if file.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", file.SchemaVersion, SchemaVersion)
	}
	if file.Agent.Name != "refund-agent" {
		t.Fatalf("Agent.Name = %q, want refund-agent", file.Agent.Name)
	}
	if len(file.AllowedActions) != 3 {
		t.Fatalf("len(AllowedActions) = %d, want 3", len(file.AllowedActions))
	}
	if file.RestrictedActions[0].Approval == nil || file.RestrictedActions[0].Approval.Source != ApprovalSourceHuman {
		t.Fatalf("restricted approval = %#v, want human approval source", file.RestrictedActions[0].Approval)
	}
	if file.UserOverrides[0].Match.Service != "stripe" {
		t.Fatalf("override service = %q, want stripe", file.UserOverrides[0].Match.Service)
	}
}

func TestLoadReadsAndValidatesBehaviorContract(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "stagehand.contract.yml")
	if err := os.WriteFile(path, []byte(validContractYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if file.ForbiddenActions[0].Reason != "destructive database write" {
		t.Fatalf("forbidden reason = %q, want destructive database write", file.ForbiddenActions[0].Reason)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
agent:
  name: refund-agent
allowed_actions:
  - service: openai
    operation: POST /v1/chat/completions
    side_effect: read
    unexpected: true
`))
	if err == nil {
		t.Fatal("Parse() expected unknown field failure")
	}
	if !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("Parse() error = %v, want unknown field message", err)
	}
}

func TestValidateRejectsInvalidRootAndActionSelectors(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v0
agent:
  name: ""
allowed_actions:
  - service: openai
    side_effect: read
  - service: stripe
    operation: POST /v1/refunds
    tool: refund_customer
    side_effect: financial
  - tool: lookup_customer
    side_effect: read
restricted_actions:
  - tool: lookup_customer
    side_effect: write
forbidden_actions:
  - service: postgres
    operation: DELETE
    side_effect: destructive
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"schema_version must be",
		"agent.name is required",
		"allowed_actions[0].operation is required",
		"allowed_actions[1].tool cannot be combined",
		"restricted_actions[0] duplicates action selector",
		"forbidden_actions[0].reason is required",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestValidateRejectsInvalidRiskAndApprovalFields(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
agent:
  name: refund-agent
restricted_actions:
  - service: stripe
    operation: POST /v1/refunds
    side_effect: risky
    allowed_fallback_tiers:
      - nearest_neighbor
      - exact
      - exact
    max_amount: -1
    approval:
      source: robot
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"restricted_actions[0].side_effect",
		"restricted_actions[0].allowed_fallback_tiers must keep tiers",
		"restricted_actions[0].allowed_fallback_tiers contains duplicate",
		"restricted_actions[0].max_amount",
		"restricted_actions[0].approval cannot be set",
		"restricted_actions[0].approval.source",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestValidateRejectsInvalidUserOverrides(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
agent:
  name: refund-agent
allowed_actions:
  - service: openai
    operation: POST /v1/chat/completions
    side_effect: read
user_overrides:
  - id: duplicate
    match:
      service: stripe
      operation: POST /v1/refunds
    approved_at: yesterday
  - id: duplicate
    match:
      tool: refund_customer
      service: stagehand.tool
    side_effect: financial
    reason: reviewed by support
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"user_overrides[0] must set at least one override field",
		"user_overrides[0].reason is required",
		"user_overrides[0].approved_at",
		`user_overrides[1].id "duplicate" is duplicated`,
		"user_overrides[1].match.tool cannot be combined",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestValidateRejectsEmptyActionLists(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
agent:
  name: refund-agent
`))
	if err == nil {
		t.Fatal("Parse() expected empty action validation failure")
	}
	if !strings.Contains(err.Error(), "at least one action is required") {
		t.Fatalf("Parse() error = %v, want empty action validation", err)
	}
}

const validContractYAML = `
schema_version: v1alpha1

agent:
  name: refund-agent

allowed_actions:
  - service: openai
    operation: POST /v1/chat/completions
    side_effect: read
    allowed_fallback_tiers:
      - exact

  - tool: lookup_customer
    side_effect: read

  - service: stripe
    operation: GET /v1/customers/search
    side_effect: read

restricted_actions:
  - service: stripe
    operation: POST /v1/refunds
    side_effect: financial
    allowed_fallback_tiers:
      - exact
      - nearest_neighbor
    max_amount: 5000
    requires_approval: true
    approval:
      source: human
      role: support_lead
      evidence_path: request.body.approval_id

forbidden_actions:
  - service: postgres
    operation: DELETE
    side_effect: destructive
    reason: destructive database write

user_overrides:
  - id: reviewed-refund-threshold
    match:
      service: stripe
      operation: POST /v1/refunds
    max_amount: 7500
    requires_approval: true
    reason: Temporary launch partner threshold approved for refund agent.
    approved_by: ops@example.com
    approved_at: 2026-06-07T12:00:00Z
`
