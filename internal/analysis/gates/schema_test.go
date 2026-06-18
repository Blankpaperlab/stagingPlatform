package gates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidReleaseGates(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(validGatesYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if file.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", file.SchemaVersion, SchemaVersion)
	}
	if len(file.ReleaseGates) != 9 {
		t.Fatalf("len(ReleaseGates) = %d, want 9", len(file.ReleaseGates))
	}
	if file.ReleaseGates[0].BlockIf == nil || file.ReleaseGates[0].BlockIf.AmountGT == nil {
		t.Fatalf("first gate block_if = %#v, want amount condition", file.ReleaseGates[0].BlockIf)
	}
	if file.ReleaseGates[2].RequireOrder == nil || file.ReleaseGates[2].RequireOrder.After.Service != "billing-api" {
		t.Fatalf("third gate require_order = %#v, want billing-api after selector", file.ReleaseGates[2].RequireOrder)
	}
	if file.ReleaseGates[8].ForbidUnknownRisk == nil {
		t.Fatalf("last gate should set forbid_unknown_risk")
	}
}

func TestLoadReadsAndValidatesReleaseGates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "stagehand.gates.yml")
	if err := os.WriteFile(path, []byte(validGatesYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if file.ReleaseGates[1].BlockIf.DestinationType != "external" {
		t.Fatalf("destination_type = %q, want external", file.ReleaseGates[1].BlockIf.DestinationType)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
release_gates:
  - name: Refund approval
    block_if:
      service: stripe
      typo: true
`))
	if err == nil {
		t.Fatal("Parse() expected unknown field failure")
	}
	if !strings.Contains(err.Error(), "field typo not found") {
		t.Fatalf("Parse() error = %v, want unknown field message", err)
	}
}

func TestValidateRejectsInvalidRootAndGateClauses(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v0
release_gates:
  - name: ""
  - name: Too many clauses
    block_if:
      side_effect: external_message
    require_order:
      before:
        tool: lookup_customer
      after:
        service: billing-api
  - name: Empty block
    block_if: {}
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"schema_version must be",
		"release_gates[0].name is required",
		"release_gates[0] must set exactly one gate clause",
		"release_gates[1] must set exactly one gate clause",
		"release_gates[2].block_if must set at least one condition field",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestValidateRejectsInvalidSelectorsAndAmounts(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
release_gates:
  - name: Bad order
    require_order:
      before:
        operation: POST /v1/refunds
      after:
        tool: update_ticket
        service: support-api
  - name: Bad max
    max_amount:
      service: stripe
      amount: -1
  - name: Bad approval
    require_approval:
      side_effect: risky
      amount_gt: -1
  - name: Bad channels
    allowed_channels:
      values: ["email", "email", ""]
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"require_order.before.service is required",
		"require_order.after.tool cannot be combined",
		"max_amount.amount must be greater",
		"require_approval.amount_gt must be greater",
		"require_approval.side_effect",
		"allowed_channels.values contains duplicate",
		"allowed_channels.values[2] cannot be empty",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

const validGatesYAML = `
schema_version: v1alpha1

release_gates:
  - name: Refunds over $50 require approval
    block_if:
      service: stripe
      operation: POST /v1/refunds
      amount_gt: 5000
      approval_missing: true

  - name: Agent cannot send external customer messages
    block_if:
      side_effect: external_message
      destination_type: external

  - name: Customer lookup must happen before billing action
    require_order:
      before:
        tool: lookup_customer
      after:
        service: billing-api

  - name: Financial actions need human approval
    require_approval:
      side_effect: financial
      amount_gt: 5000

  - name: Refund ceiling
    max_amount:
      service: stripe
      operation: POST /v1/refunds
      amount: 5000

  - name: Allowed customer message channels
    allowed_channels:
      side_effect: external_message
      values:
        - email
        - sms

  - name: Allowed notification domains
    allowed_domains:
      side_effect: external_message
      values:
        - example.com
        - support.example.com

  - name: No new financial actions
    forbid_new_action:
      side_effect: financial

  - name: Unknown risk blocks release
    forbid_unknown_risk: {}
`
