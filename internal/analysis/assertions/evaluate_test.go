package assertions

import (
	"testing"
	"time"

	"stagehand/internal/recorder"
)

func TestEvaluateCoreAssertionsPass(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: count-payment-intents
    type: count
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
  - id: card-decline-code
    type: payload-field
    match:
      interaction_id: int_payment_3
    expect:
      path: response.body.error.code
      equals: card_declined
  - id: no-charge-create
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
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("len(results) = %d, want 5", len(results))
	}
	if !AllPassed(results) {
		t.Fatalf("AllPassed() = false, results = %#v", results)
	}

	countResult := resultByID(t, results, "count-payment-intents")
	if countResult.Evidence.Count != 3 {
		t.Fatalf("count evidence = %d, want 3", countResult.Evidence.Count)
	}

	payloadResult := resultByID(t, results, "card-decline-code")
	if payloadResult.Evidence.Actual != "card_declined" {
		t.Fatalf("payload actual = %#v, want card_declined", payloadResult.Evidence.Actual)
	}

	orderingResult := resultByID(t, results, "customer-before-payment")
	if orderingResult.Evidence.Before == nil || orderingResult.Evidence.After == nil {
		t.Fatalf("ordering evidence = %#v, want before and after", orderingResult.Evidence)
	}
	if orderingResult.Evidence.Before.InteractionID != "int_customer" || orderingResult.Evidence.After.InteractionID != "int_payment_1" {
		t.Fatalf("ordering evidence = %#v, want customer before first payment", orderingResult.Evidence)
	}
}

func TestEvaluateCoreAssertionsFailWithEvidence(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: count-too-high
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        min: 4
  - id: impossible-order
    type: ordering
    before:
      interaction_id: int_payment_1
    after:
      interaction_id: int_customer
  - id: wrong-decline-code
    type: payload-field
    match:
      interaction_id: int_payment_3
    expect:
      path: response.body.error.code
      equals: expired_card
  - id: forbidden-customer
    type: forbidden-operation
    match:
      service: stripe
      operation: customers.create
  - id: forbid-nearest
    type: fallback-prohibition
    match:
      service: stripe
    expect:
      disallowed_tiers:
        - nearest_neighbor
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if AllPassed(results) {
		t.Fatalf("AllPassed() = true, want failures: %#v", results)
	}

	for _, result := range results {
		if result.Status != ResultStatusFailed {
			t.Fatalf("result %q status = %q, want failed", result.AssertionID, result.Status)
		}
		if result.Message == "" {
			t.Fatalf("result %q has empty message", result.AssertionID)
		}
	}

	forbidden := resultByID(t, results, "forbidden-customer")
	if len(forbidden.Evidence.Violations) != 1 || forbidden.Evidence.Violations[0].InteractionID != "int_customer" {
		t.Fatalf("forbidden evidence = %#v, want int_customer violation", forbidden.Evidence.Violations)
	}

	fallback := resultByID(t, results, "forbid-nearest")
	if len(fallback.Evidence.Violations) != 1 || fallback.Evidence.Violations[0].InteractionID != "int_payment_2" {
		t.Fatalf("fallback evidence = %#v, want int_payment_2 violation", fallback.Evidence.Violations)
	}
}

func TestEvaluatePayloadFieldSupportsRequestPathsAndExists(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: request-customer-present
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: request.body.customer
      equals: cus_123
  - id: missing-field-absent
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: request.body.missing
      exists: false
  - id: event-index-path
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: events[1].data.status
      equals: 200
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !AllPassed(results) {
		t.Fatalf("AllPassed() = false, results = %#v", results)
	}
}

func TestEvaluateCountSupportsMaxAndRangeBounds(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: max-only-passes
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        max: 5
  - id: max-only-fails
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        max: 2
  - id: range-passes
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        min: 2
        max: 5
  - id: range-fails-min
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        min: 4
        max: 10
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	statuses := map[string]ResultStatus{}
	for _, result := range results {
		statuses[result.AssertionID] = result.Status
	}
	want := map[string]ResultStatus{
		"max-only-passes": ResultStatusPassed,
		"max-only-fails":  ResultStatusFailed,
		"range-passes":    ResultStatusPassed,
		"range-fails-min": ResultStatusFailed,
	}
	for id, expected := range want {
		if statuses[id] != expected {
			t.Fatalf("status(%q) = %q, want %q", id, statuses[id], expected)
		}
	}
}

func TestEvaluateRejectsCountExpectationsCombiningEqualsWithBounds(t *testing.T) {
	t.Parallel()

	if _, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: invalid-equals-and-min
    type: count
    match:
      service: stripe
      operation: payment_intents.create
    expect:
      count:
        equals: 3
        min: 2
`)); err == nil {
		t.Fatal("Parse() expected validation error combining equals with min")
	}
}

func TestEvaluatePayloadFieldNotEqualsAndDeepPaths(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: status-not-pending
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: response.body.status
      not_equals: pending
  - id: status-not-equals-fails
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: response.body.status
      not_equals: requires_confirmation
  - id: deep-event-data-status
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: events[1].data.status
      equals: 200
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	statuses := map[string]ResultStatus{}
	for _, result := range results {
		statuses[result.AssertionID] = result.Status
	}
	want := map[string]ResultStatus{
		"status-not-pending":      ResultStatusPassed,
		"status-not-equals-fails": ResultStatusFailed,
		"deep-event-data-status":  ResultStatusPassed,
	}
	for id, expected := range want {
		if statuses[id] != expected {
			t.Fatalf("status(%q) = %q, want %q", id, statuses[id], expected)
		}
	}
}

func TestEvaluatePayloadFieldHandlesMissingPaths(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: missing-path-equals-fails
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: response.body.does.not.exist
      equals: anything
  - id: missing-path-exists-false-passes
    type: payload-field
    match:
      interaction_id: int_payment_1
    expect:
      path: response.body.does.not.exist
      exists: false
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	statuses := map[string]ResultStatus{}
	for _, result := range results {
		statuses[result.AssertionID] = result.Status
	}
	if statuses["missing-path-equals-fails"] != ResultStatusFailed {
		t.Fatalf("missing path with equals should fail, got %q", statuses["missing-path-equals-fails"])
	}
	if statuses["missing-path-exists-false-passes"] != ResultStatusPassed {
		t.Fatalf("missing path with exists:false should pass, got %q", statuses["missing-path-exists-false-passes"])
	}
}

func TestEvaluateForbiddenOperationPassesWhenNoMatches(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: no-charges
    type: forbidden-operation
    match:
      service: stripe
      operation: charges.create
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !AllPassed(results) {
		t.Fatalf("AllPassed() = false, results = %#v", results)
	}
	if results[0].Evidence.Count != 0 {
		t.Fatalf("forbidden evidence count = %d, want 0", results[0].Evidence.Count)
	}
}

func TestEvaluateCrossServiceAssertions(t *testing.T) {
	t.Parallel()

	file, err := Parse([]byte(`
schema_version: v1alpha1
assertions:
  - id: customer-linked
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
  - id: customer-missing-link
    type: cross-service
    left:
      service: stripe
      operation: customers.create
      path: response.body.id
    right:
      service: stripe
      operation: payment_intents.create
      path: response.body.id
    relationship: equals
  - id: nested-object-link
    type: cross-service
    left:
      service: stripe
      operation: customers.create
      path: response.body
    right:
      service: stripe
      operation: payment_intents.create
      path: request.body.customer
    relationship: equals
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	results, err := Evaluate(sampleRun(), file)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	linked := resultByID(t, results, "customer-linked")
	if linked.Status != ResultStatusPassed {
		t.Fatalf("customer-linked status = %q, want passed", linked.Status)
	}
	if len(linked.Evidence.LinkedEntities) != 3 {
		t.Fatalf("linked pair count = %d, want 3", len(linked.Evidence.LinkedEntities))
	}
	if linked.Evidence.LinkedEntities[0].Left.Interaction.InteractionID != "int_customer" {
		t.Fatalf("left linked evidence = %#v, want int_customer", linked.Evidence.LinkedEntities[0].Left.Interaction)
	}
	if linked.Evidence.LinkedEntities[0].Right.Interaction.InteractionID != "int_payment_1" {
		t.Fatalf("right linked evidence = %#v, want int_payment_1", linked.Evidence.LinkedEntities[0].Right.Interaction)
	}

	missing := resultByID(t, results, "customer-missing-link")
	if missing.Status != ResultStatusFailed {
		t.Fatalf("customer-missing-link status = %q, want failed", missing.Status)
	}
	if len(missing.Evidence.LeftEntities) == 0 || len(missing.Evidence.RightEntities) == 0 {
		t.Fatalf("missing-link evidence = %#v, want both sides populated", missing.Evidence)
	}

	nested := resultByID(t, results, "nested-object-link")
	if nested.Status != ResultStatusPassed {
		t.Fatalf("nested-object-link status = %q, want passed", nested.Status)
	}
	if AllPassed(results) {
		t.Fatal("AllPassed() = true with a failing cross-service assertion")
	}
}

func resultByID(t *testing.T, results []Result, id string) Result {
	t.Helper()

	for _, result := range results {
		if result.AssertionID == id {
			return result
		}
	}
	t.Fatalf("missing result %q in %#v", id, results)
	return Result{}
}

func sampleRun() recorder.Run {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	return recorder.Run{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_assertions",
		SessionName:        "assertions",
		Mode:               recorder.RunModeReplay,
		Status:             recorder.RunStatusComplete,
		StartedAt:          now,
		EndedAt:            &now,
		Interactions: []recorder.Interaction{
			sampleInteraction("int_customer", 1, "customers.create", recorder.FallbackTierExact, map[string]any{
				"email": "customer@example.com",
			}, map[string]any{
				"id": "cus_123",
			}),
			sampleInteraction("int_payment_1", 2, "payment_intents.create", recorder.FallbackTierExact, map[string]any{
				"amount":   1000,
				"customer": "cus_123",
			}, map[string]any{
				"id":     "pi_001",
				"status": "requires_confirmation",
			}),
			sampleInteraction("int_payment_2", 3, "payment_intents.create", recorder.FallbackTierNearestNeighbor, map[string]any{
				"amount":   2000,
				"customer": "cus_123",
			}, map[string]any{
				"id":     "pi_002",
				"status": "requires_confirmation",
			}),
			sampleInteraction("int_payment_3", 4, "payment_intents.create", recorder.FallbackTierExact, map[string]any{
				"amount":   3000,
				"customer": "cus_123",
			}, map[string]any{
				"error": map[string]any{
					"code":    "card_declined",
					"message": "Your card was declined.",
				},
			}),
		},
	}
}

func sampleInteraction(
	interactionID string,
	sequence int,
	operation string,
	fallbackTier recorder.FallbackTier,
	requestBody map[string]any,
	responseBody map[string]any,
) recorder.Interaction {
	return recorder.Interaction{
		RunID:         "run_assertions",
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       "stripe",
		Operation:     operation,
		Protocol:      recorder.ProtocolHTTPS,
		FallbackTier:  fallbackTier,
		Request: recorder.Request{
			Method: "POST",
			URL:    "https://api.stripe.com/v1/" + operation,
			Body:   requestBody,
		},
		Events: []recorder.Event{
			{
				Sequence: 1,
				Type:     recorder.EventTypeRequestSent,
				Data: map[string]any{
					"method": "POST",
				},
			},
			{
				Sequence: 2,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status": 200,
					"body":   responseBody,
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_assertions",
		},
	}
}
