package assertions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidAssertionFile(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(validAssertionYAML))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if file.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", file.SchemaVersion, SchemaVersion)
	}
	if len(file.Assertions) != 6 {
		t.Fatalf("len(Assertions) = %d, want 6", len(file.Assertions))
	}
	if file.Assertions[0].ID != "exactly-three-payment-intents" {
		t.Fatalf("first assertion ID = %q, want exactly-three-payment-intents", file.Assertions[0].ID)
	}
}

func TestLoadReadsAndValidatesAssertionFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "stagehand.assertions.yml")
	if err := os.WriteFile(path, []byte(validAssertionYAML), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if file.Assertions[5].Type != TypeCrossService {
		t.Fatalf("last assertion type = %q, want %q", file.Assertions[5].Type, TypeCrossService)
	}
}

func TestParseRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: unknown-field
    type: count
    match:
      service: stripe
    expect:
      count:
        equals: 1
    typo: true
`))
	if err == nil {
		t.Fatal("Parse() expected unknown field failure")
	}
	if !strings.Contains(err.Error(), "field typo not found") {
		t.Fatalf("Parse() error = %v, want unknown field message", err)
	}
}

func TestValidateReturnsClearFieldCombinationErrors(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: bad-count
    type: count
    match:
      service: stripe
    expect:
      count:
        equals: 1
        min: 1
  - id: bad-ordering
    type: ordering
    match:
      service: stripe
    before:
      service: stripe
      operation: customers.create
    expect:
      count:
        equals: 1
  - id: bad-payload
    type: payload-field
    match:
      service: stripe
    expect:
      path: response.body.error.code
      equals: card_declined
      exists: true
  - id: bad-cross-service
    type: cross-service
    left:
      service: stripe
      operation: customers.create
      path: response.body.id
    relationship: contains
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"assertions[0].expect.count cannot combine equals with min or max",
		"assertions[1].match is not allowed for ordering assertions",
		"assertions[1].after is required for ordering assertions",
		"assertions[1].expect is not allowed",
		"assertions[2].expect must set exactly one",
		"assertions[3].right is required",
		"assertions[3].relationship must be",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestValidateRejectsInvalidRootAndDuplicateIDs(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v0
assertions:
  - id: duplicate
    type: unsupported
  - id: duplicate
    type: forbidden-operation
    match:
      service: stripe
`))
	if err == nil {
		t.Fatal("Parse() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"schema_version must be",
		"assertions[0].type must be one of",
		`assertions[1].id "duplicate" is duplicated`,
		"assertions[1].match.operation is required",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestValidateRejectsEmptyAssertionList(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
assertions: []
`))
	if err == nil {
		t.Fatal("Parse() expected empty assertions failure")
	}
	if !strings.Contains(err.Error(), "assertions must contain at least one assertion") {
		t.Fatalf("Parse() error = %v, want empty list validation", err)
	}
}

func TestValidateRejectsMalformedAssertionPaths(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: bad-payload-path
    type: payload-field
    match:
      service: stripe
    expect:
      path: response..body
      exists: true
  - id: bad-cross-service-path
    type: cross-service
    left:
      service: stripe
      operation: customers.create
      path: response.body.id
    right:
      service: stripe
      operation: payment_intents.create
      path: request.body.
    relationship: equals
`))
	if err == nil {
		t.Fatal("Parse() expected malformed path failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"assertions[0].expect.path is malformed",
		"assertions[1].right.path is malformed",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

const validAssertionYAML = `
schema_version: v1alpha1
assertions:
  - id: exactly-three-payment-intents
    type: count
    description: Payment flow should create three successful intents.
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        equals: 3

  - id: customer-before-payment
    type: ordering
    before:
      service: stripe
      operation: customers.create
    after:
      service: stripe
      operation: payment_intents.create

  - id: declined-code-present
    type: payload-field
    match:
      service: stripe
      operation: payment_intents.create
      event_type: response_received
    expect:
      path: response.body.error.code
      equals: card_declined

  - id: no-charge-endpoint
    type: forbidden-operation
    match:
      service: stripe
      operation: charges.create

  - id: no-llm-fallback
    type: fallback-prohibition
    match:
      service: stripe
    expect:
      disallowed_tiers:
        - llm_synthesis

  - id: customer-linked-to-payment
    type: cross-service
    left:
      service: stripe
      operation: customers.create
      path: response.body.id
    right:
      service: stripe
      operation: payment_intents.create
      path: request.body.customer
    relationship: equals
`
