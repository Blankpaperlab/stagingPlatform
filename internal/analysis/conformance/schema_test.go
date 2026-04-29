package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidCaseFile(t *testing.T) {
	file, err := Parse([]byte(validCaseYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(file.Cases) != 2 {
		t.Fatalf("len(Cases) = %d, want 2", len(file.Cases))
	}
	if file.Cases[0].ID != "stripe-payment-intent-happy-path" {
		t.Fatalf("first case ID = %q", file.Cases[0].ID)
	}
	if file.Cases[0].Comparison.Match.Strategy != MatchStrategyOperationSequence {
		t.Fatalf("match strategy = %q", file.Cases[0].Comparison.Match.Strategy)
	}
	if got := file.Cases[0].RealService.Credentials[0].Env; got != "STRIPE_SECRET_KEY" {
		t.Fatalf("credential env = %q, want STRIPE_SECRET_KEY", got)
	}
}

func TestLoadReadsAndValidatesCaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stripe.conformance.yml")
	if err := os.WriteFile(path, []byte(validCaseYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if file.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", file.SchemaVersion, SchemaVersion)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`
schema_version: v1alpha1
cases:
  - id: bad
    service: stripe
    unexpected: true
    inputs:
      steps:
        - id: create
          operation: customers.create
    comparison:
      match:
        strategy: operation_sequence
`))
	if err == nil {
		t.Fatal("Parse() expected unknown-field error")
	}
	if !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestValidateRejectsInvalidCases(t *testing.T) {
	_, err := Parse([]byte(`
schema_version: v1alpha1
cases:
  - id: duplicate
    service: stripe
    inputs:
      steps:
        - id: create
    real_service:
      credentials:
        - required: true
    comparison:
      match:
        strategy: interaction_identity
      tolerated_diff_fields:
        - path: response.body.id
  - id: duplicate
    inputs:
      steps: []
    comparison:
      match:
        strategy: unknown
`))
	if err == nil {
		t.Fatal("Parse() expected validation error")
	}
	for _, expected := range []string{
		"cases[0].inputs.steps[0].operation is required",
		"cases[0].real_service.credentials[0].env is required",
		"cases[0].real_service.credentials[0].purpose is required",
		`cases[0].comparison.match.keys must contain at least one key for "interaction_identity"`,
		"cases[0].comparison.tolerated_diff_fields[0].reason is required",
		`cases[1].id "duplicate" is duplicated`,
		"cases[1].service is required",
		"cases[1].inputs.steps must contain at least one step",
		"cases[1].comparison.match.strategy must be one of",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("Parse() error missing %q\nerror=%v", expected, err)
		}
	}
}

func TestValidateRejectsEmptyCaseList(t *testing.T) {
	_, err := Parse([]byte(`
schema_version: v1alpha1
cases: []
`))
	if err == nil {
		t.Fatal("Parse() expected empty case list failure")
	}
	if !strings.Contains(err.Error(), "cases must contain at least one case") {
		t.Fatalf("Parse() error = %v", err)
	}
}

const validCaseYAML = `
schema_version: v1alpha1
cases:
  - id: stripe-payment-intent-happy-path
    service: stripe
    description: Create a customer, payment method, and succeeded payment intent.
    tags:
      - smoke
      - payment_intent
    inputs:
      scenario: payment_intent_happy_path
      steps:
        - id: create-customer
          operation: customers.create
          request:
            email: conformance@example.com
          expect:
            status_code: 200
        - id: create-payment-intent
          operation: payment_intents.create
          request:
            amount: 1200
            currency: usd
          expect:
            status: succeeded
    real_service:
      network: true
      test_mode: true
      cost_estimate: no charge in Stripe test mode
      credentials:
        - env: STRIPE_SECRET_KEY
          required: true
          purpose: Stripe test-mode API key.
    comparison:
      match:
        strategy: operation_sequence
      tolerated_diff_fields:
        - path: response.body.id
          reason: Real Stripe and simulator generate different object IDs.
          applies_to:
            - customers.create
            - payment_intents.create
        - path: response.body.created
          reason: Real service timestamps are wall-clock values.
  - id: openai-chat-smoke
    service: openai
    inputs:
      steps:
        - id: create-chat
          operation: chat.completions.create
          request:
            model: gpt-5.4-mini
            messages:
              - role: user
                content: Reply with pong.
          expect:
            status_code: 200
    real_service:
      network: true
      credentials:
        - env: OPENAI_API_KEY
          required: true
          purpose: OpenAI API key for nightly conformance.
    comparison:
      match:
        strategy: interaction_identity
        keys:
          - service
          - operation
          - request.body.model
      tolerated_diff_fields:
        - path: response.body.id
          reason: Response IDs are generated by the service.
`
