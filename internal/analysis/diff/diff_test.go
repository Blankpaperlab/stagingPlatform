package diff

import (
	"testing"
	"time"

	"stagehand/internal/recorder"
)

func TestCompareRequiresMatchingSession(t *testing.T) {
	t.Parallel()

	_, err := Compare(runFixture("run_base", "left", nil), runFixture("run_candidate", "right", nil), Options{})
	if err == nil {
		t.Fatal("Compare() expected session mismatch error")
	}
}

func TestCompareDetectsAddedAndRemovedInteractions(t *testing.T) {
	t.Parallel()

	base := runFixture("run_base", "session-a", []recorder.Interaction{
		interactionFixture("run_base", "base_keep", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{"prompt": "hello"}, ""),
		interactionFixture("run_base", "base_removed", 2, "stripe", "customers.create", "/v1/customers", map[string]any{"email": "a@example.com"}, ""),
	})
	candidate := runFixture("run_candidate", "session-a", []recorder.Interaction{
		interactionFixture("run_candidate", "candidate_keep", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{"prompt": "hello"}, ""),
		interactionFixture("run_candidate", "candidate_added", 3, "stripe", "payment_intents.create", "/v1/payment_intents", map[string]any{"amount": 1000}, ""),
	})

	result, err := Compare(base, candidate, Options{})
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	assertChangeTypes(t, result.Changes, []ChangeType{ChangeRemoved, ChangeAdded})
}

func TestCompareDetectsOrderingChanges(t *testing.T) {
	t.Parallel()

	first := interactionFixture("run_base", "base_first", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{"prompt": "first"}, "")
	second := interactionFixture("run_base", "base_second", 2, "stripe", "customers.create", "/v1/customers", map[string]any{"email": "a@example.com"}, "")
	candidateSecond := second
	candidateSecond.RunID = "run_candidate"
	candidateSecond.InteractionID = "candidate_second"
	candidateSecond.Sequence = 1
	candidateFirst := first
	candidateFirst.RunID = "run_candidate"
	candidateFirst.InteractionID = "candidate_first"
	candidateFirst.Sequence = 2

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{first, second}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidateSecond, candidateFirst}),
		Options{},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	assertChangeTypes(t, result.Changes, []ChangeType{ChangeOrderingChanged, ChangeOrderingChanged})
}

func TestCompareDetectsFallbackRegression(t *testing.T) {
	t.Parallel()

	baseInteraction := interactionFixture("run_base", "base_int", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{"prompt": "hello"}, recorder.FallbackTierExact)
	candidateInteraction := baseInteraction
	candidateInteraction.RunID = "run_candidate"
	candidateInteraction.InteractionID = "candidate_int"
	candidateInteraction.FallbackTier = recorder.FallbackTierNearestNeighbor

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{baseInteraction}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidateInteraction}),
		Options{},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	assertChangeTypes(t, result.Changes, []ChangeType{ChangeFallbackRegression})
	if result.Changes[0].CandidateFallbackTier != recorder.FallbackTierNearestNeighbor {
		t.Fatalf("CandidateFallbackTier = %q, want nearest_neighbor", result.Changes[0].CandidateFallbackTier)
	}
}

func TestCompareIgnoresConfiguredFields(t *testing.T) {
	t.Parallel()

	baseInteraction := interactionFixture("run_base", "base_int", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{
		"prompt":     "hello",
		"request_id": "req_base",
	}, "")
	candidateInteraction := interactionFixture("run_candidate", "candidate_int", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{
		"prompt":     "hello",
		"request_id": "req_candidate",
	}, "")

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{baseInteraction}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidateInteraction}),
		Options{IgnoredFields: []string{"request.body.request_id"}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("len(Changes) = %d, want 0: %#v", len(result.Changes), result.Changes)
	}
}

func TestCompareIgnoresConfiguredRequestHeaderFields(t *testing.T) {
	t.Parallel()

	baseInteraction := interactionFixture("run_base", "base_int", 1, "stripe", "payment_intents.create", "/v1/payment_intents", map[string]any{}, "")
	baseInteraction.Request.Headers = map[string][]string{
		"idempotency-key": {"ik_base"},
		"stripe-version":  {"2025-11-05"},
	}
	candidateInteraction := interactionFixture("run_candidate", "candidate_int", 1, "stripe", "payment_intents.create", "/v1/payment_intents", map[string]any{}, "")
	candidateInteraction.Request.Headers = map[string][]string{
		"idempotency-key": {"ik_candidate"},
		"stripe-version":  {"2025-11-05"},
	}

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{baseInteraction}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidateInteraction}),
		Options{IgnoredFields: []string{"request.headers.idempotency-key"}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("request header ignore produced changes: %#v", result.Changes)
	}
}

func TestCompareDoesNotFlagTimingOnlyDifferencesAsModified(t *testing.T) {
	t.Parallel()

	base := timingPollutionFixture("run_base", 0, 1, 1)
	candidate := timingPollutionFixture("run_candidate", 0, 7, 12)

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{base}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidate}),
		Options{},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf(
			"timing-only differences produced %d change(s); want 0 because the same logical interaction was replayed: %#v",
			len(result.Changes),
			result.Changes,
		)
	}
}

func TestCompareDoesNotFlagScrubEvidenceOnlyDifferencesAsModified(t *testing.T) {
	t.Parallel()

	base := interactionFixture("run_base", "base_int", 1, "stripe", "customers.search", "/v1/customers/search", map[string]any{}, "")
	base.ScrubReport.DetectorKinds = []string{"email"}
	base.ScrubReport.RedactedPaths = []string{"request.query.query"}
	base.ScrubReport.SessionSaltID = "salt_base"

	candidate := interactionFixture("run_candidate", "candidate_int", 1, "stripe", "customers.search", "/v1/customers/search", map[string]any{}, "")
	candidate.ScrubReport.DetectorKinds = []string{"email", "phone", "jwt"}
	candidate.ScrubReport.RedactedPaths = []string{
		"request.query.query",
		"events[1].data.body.data[0].email",
		"events[1].data.body.data[0].phone",
	}
	candidate.ScrubReport.SessionSaltID = "salt_candidate"

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{base}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidate}),
		Options{},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("scrub evidence-only differences produced changes: %#v", result.Changes)
	}
}

func TestCompareCanIgnoreEventArrayFieldsViaPathTraversal(t *testing.T) {
	t.Parallel()

	base := timingPollutionFixture("run_base", 0, 1, 0)
	candidate := timingPollutionFixture("run_candidate", 0, 7, 0)

	for _, ignorePath := range []string{"events.t_ms", "events[*].t_ms", "events.0.t_ms"} {
		t.Run(ignorePath, func(t *testing.T) {
			result, err := Compare(
				runFixture("run_base", "session-a", []recorder.Interaction{base}),
				runFixture("run_candidate", "session-a", []recorder.Interaction{candidate}),
				Options{IgnoredFields: []string{ignorePath, "events[*].sim_t_ms"}},
			)
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if len(result.Changes) != 0 {
				t.Fatalf("ignore path %q produced changes after event-array timing suppression: %#v", ignorePath, result.Changes)
			}
		})
	}
}

func TestCompareResponseBodyIgnoreFieldMatchesResponseEventBody(t *testing.T) {
	t.Parallel()

	base := interactionFixture("run_base", "base_int", 1, "stripe", "customers.create", "/v1/customers", map[string]any{}, "")
	base.Events[1].Data = map[string]any{
		"body": map[string]any{
			"id":    "cus_base",
			"email": "customer@example.com",
		},
	}
	candidate := interactionFixture("run_candidate", "candidate_int", 1, "stripe", "customers.create", "/v1/customers", map[string]any{}, "")
	candidate.Events[1].Data = map[string]any{
		"body": map[string]any{
			"id":    "cus_candidate",
			"email": "customer@example.com",
		},
	}

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{base}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidate}),
		Options{IgnoredFields: []string{"response.body.id"}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("response.body ignore alias produced changes: %#v", result.Changes)
	}
}

func TestCompareResponseBodyIgnoreFieldCanRemoveWholeResponseEventBody(t *testing.T) {
	t.Parallel()

	base := interactionFixture("run_base", "base_int", 1, "stripe", "payment_intents.create", "/v1/payment_intents", map[string]any{}, "")
	base.Events[1].Data = map[string]any{
		"body": map[string]any{
			"id":                   "pi_base",
			"latest_charge":        "ch_base",
			"processor_reference":  "base_dynamic_value",
			"payment_method_types": []any{"card"},
		},
	}
	candidate := interactionFixture("run_candidate", "candidate_int", 1, "stripe", "payment_intents.create", "/v1/payment_intents", map[string]any{}, "")
	candidate.Events[1].Data = map[string]any{
		"body": map[string]any{
			"id":                   "pi_candidate",
			"latest_charge":        "ch_candidate",
			"processor_reference":  "candidate_dynamic_value",
			"payment_method_types": []any{"card"},
		},
	}

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{base}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidate}),
		Options{IgnoredFields: []string{"response.body"}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("response.body ignore alias produced changes: %#v", result.Changes)
	}
}

func TestCompareResponseHeadersIgnoreFieldMatchesResponseEventHeaders(t *testing.T) {
	t.Parallel()

	base := interactionFixture("run_base", "base_int", 1, "stripe", "customers.create", "/v1/customers", map[string]any{}, "")
	base.Events[1].Data = map[string]any{
		"headers": map[string]any{
			"request-id": "req_base",
		},
		"body": map[string]any{
			"id": "cus_same",
		},
	}
	candidate := interactionFixture("run_candidate", "candidate_int", 1, "stripe", "customers.create", "/v1/customers", map[string]any{}, "")
	candidate.Events[1].Data = map[string]any{
		"headers": map[string]any{
			"request-id": "req_candidate",
		},
		"body": map[string]any{
			"id": "cus_same",
		},
	}

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{base}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidate}),
		Options{IgnoredFields: []string{"response.headers"}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	if len(result.Changes) != 0 {
		t.Fatalf("response.headers ignore alias produced changes: %#v", result.Changes)
	}
}

func TestCompareReportsModifiedRatherThanReorderForSameEndpointBodyChange(t *testing.T) {
	t.Parallel()

	baseFirst := interactionFixture("run_base", "base_first", 1, "stripe", "customers.create", "/v1/customers", map[string]any{"email": "a@example.com"}, "")
	baseSecond := interactionFixture("run_base", "base_second", 2, "stripe", "customers.create", "/v1/customers", map[string]any{"email": "b@example.com"}, "")
	candidateFirst := baseSecond
	candidateFirst.RunID = "run_candidate"
	candidateFirst.InteractionID = "candidate_first"
	candidateFirst.Sequence = 1
	candidateSecond := baseFirst
	candidateSecond.RunID = "run_candidate"
	candidateSecond.InteractionID = "candidate_second"
	candidateSecond.Sequence = 2

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{baseFirst, baseSecond}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidateFirst, candidateSecond}),
		Options{},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	gotTypes := make([]ChangeType, 0, len(result.Changes))
	for _, change := range result.Changes {
		gotTypes = append(gotTypes, change.Type)
	}
	for _, change := range result.Changes {
		if change.Type == ChangeOrderingChanged {
			t.Fatalf("alignment by occurrence count keyed sibling requests by lane order, so reordering same-endpoint requests with different bodies should report modified, not ordering_changed: %#v", change)
		}
	}
	if len(gotTypes) != 2 {
		t.Fatalf("change types = %#v, want two modified changes", gotTypes)
	}
}

func timingPollutionFixture(runID string, requestSent, responseAt, latency int64) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: runID + "_int",
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Body:   map[string]any{"prompt": "hello"},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: requestSent, SimTMS: requestSent, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: responseAt, SimTMS: responseAt, Type: recorder.EventTypeResponseReceived},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_test",
		},
		LatencyMS: latency,
	}
}

func TestCompareDetectsModifiedInteractionWhenFieldsAreNotIgnored(t *testing.T) {
	t.Parallel()

	baseInteraction := interactionFixture("run_base", "base_int", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{"prompt": "hello"}, "")
	candidateInteraction := interactionFixture("run_candidate", "candidate_int", 1, "openai", "chat.completions.create", "/v1/chat/completions", map[string]any{"prompt": "goodbye"}, "")

	result, err := Compare(
		runFixture("run_base", "session-a", []recorder.Interaction{baseInteraction}),
		runFixture("run_candidate", "session-a", []recorder.Interaction{candidateInteraction}),
		Options{IgnoredFields: []string{"run_id", "interaction_id"}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}

	assertChangeTypes(t, result.Changes, []ChangeType{ChangeModified})
}

func assertChangeTypes(t *testing.T, changes []Change, want []ChangeType) {
	t.Helper()

	got := make([]ChangeType, 0, len(changes))
	for _, change := range changes {
		got = append(got, change.Type)
	}
	if len(got) != len(want) {
		t.Fatalf("change types = %#v, want %#v", got, want)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("change types = %#v, want %#v", got, want)
		}
	}
}

func runFixture(runID, session string, interactions []recorder.Interaction) recorder.Run {
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	ended := now.Add(time.Second)
	return recorder.Run{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "test",
		RuntimeVersion:     "test",
		ScrubPolicyVersion: "v1",
		RunID:              runID,
		SessionName:        session,
		Mode:               recorder.RunModeRecord,
		Status:             recorder.RunStatusComplete,
		StartedAt:          now,
		EndedAt:            &ended,
		Interactions:       interactions,
	}
}

func interactionFixture(
	runID string,
	interactionID string,
	sequence int,
	service string,
	operation string,
	path string,
	body map[string]any,
	fallbackTier recorder.FallbackTier,
) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       service,
		Operation:     operation,
		Protocol:      recorder.ProtocolHTTPS,
		FallbackTier:  fallbackTier,
		Request: recorder.Request{
			URL:    "https://api.example.test" + path,
			Method: "POST",
			Body:   body,
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: 1, SimTMS: 1, Type: recorder.EventTypeResponseReceived},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_test",
		},
		LatencyMS: 1,
	}
}
