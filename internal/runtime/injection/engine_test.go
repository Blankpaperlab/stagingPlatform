package injection

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestEngineInjectsDeterministicThirdCallFailure(t *testing.T) {
	t.Parallel()

	engine, err := NewEngine([]Rule{
		{
			Match: Match{
				Service:   "stripe",
				Operation: "payment_intents.create",
				NthCall:   3,
			},
			Inject: Inject{
				Library: "stripe.card_declined",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	request := Request{Service: "stripe", Operation: "payment_intents.create"}
	for call := 1; call <= 2; call++ {
		decision, err := engine.Evaluate(request)
		if err != nil {
			t.Fatalf("Evaluate(call %d) error = %v", call, err)
		}
		if decision.Matched {
			t.Fatalf("Evaluate(call %d) matched unexpectedly: %#v", call, decision)
		}
	}

	decision, err := engine.Evaluate(request)
	if err != nil {
		t.Fatalf("Evaluate(third call) error = %v", err)
	}
	if !decision.Matched {
		t.Fatal("Evaluate(third call) did not match")
	}
	if decision.Override.Status != 402 {
		t.Fatalf("Override.Status = %d, want 402", decision.Override.Status)
	}
	errorBody := decision.Override.Body["error"].(map[string]any)
	if errorBody["code"] != "card_declined" {
		t.Fatalf("error code = %q, want card_declined", errorBody["code"])
	}
	if decision.Provenance.RuleIndex != 0 || decision.Provenance.CallNumber != 3 || decision.Provenance.Library != "stripe.card_declined" {
		t.Fatalf("Provenance = %#v, want rule 0 call 3 library stripe.card_declined", decision.Provenance)
	}

	decision, err = engine.Evaluate(request)
	if err != nil {
		t.Fatalf("Evaluate(fourth call) error = %v", err)
	}
	if decision.Matched {
		t.Fatalf("Evaluate(fourth call) matched unexpectedly: %#v", decision)
	}
}

func TestEngineSupportsProbabilityAndExplicitResponseOverride(t *testing.T) {
	t.Parallel()

	never := 0.0
	always := 1.0
	engine, err := NewEngine([]Rule{
		{
			Match: Match{
				Service:     "stripe",
				Operation:   "customers.create",
				AnyCall:     true,
				Probability: &never,
			},
			Inject: Inject{
				Status: 503,
				Body: map[string]any{
					"error": map[string]any{"code": "never"},
				},
			},
		},
		{
			Match: Match{
				Service:     "stripe",
				Operation:   "customers.create",
				AnyCall:     true,
				Probability: &always,
			},
			Inject: Inject{
				Status: 429,
				Body: map[string]any{
					"error": map[string]any{"code": "rate_limit_override"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	decision, err := engine.Evaluate(Request{Service: "stripe", Operation: "customers.create"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Matched {
		t.Fatal("Evaluate() did not match")
	}
	if decision.Provenance.RuleIndex != 1 {
		t.Fatalf("RuleIndex = %d, want 1", decision.Provenance.RuleIndex)
	}
	if decision.Override.Status != 429 {
		t.Fatalf("Override.Status = %d, want 429", decision.Override.Status)
	}
	errorBody := decision.Override.Body["error"].(map[string]any)
	if errorBody["code"] != "rate_limit_override" {
		t.Fatalf("error code = %q, want rate_limit_override", errorBody["code"])
	}
}

func TestNamedLibraryEntriesDeepCloneNestedBodies(t *testing.T) {
	t.Parallel()

	entries := NamedLibraryEntries()
	cardDeclined := entries["stripe.card_declined"]
	errorBody := cardDeclined.Body["error"].(map[string]any)
	errorBody["code"] = "mutated"

	next, ok := NamedLibrary("stripe.card_declined")
	if !ok {
		t.Fatal("NamedLibrary(stripe.card_declined) ok = false")
	}
	nextErrorBody := next.Body["error"].(map[string]any)
	if nextErrorBody["code"] != "card_declined" {
		t.Fatalf("NamedLibrary() nested code = %#v, want card_declined", nextErrorBody["code"])
	}
}

func TestEngineFromConfigUsesNamedLibraryEntries(t *testing.T) {
	t.Parallel()

	cfg := config.ErrorInjectionConfig{
		Enabled: true,
		Rules: []config.ErrorInjectionRule{
			{
				Match: config.ErrorMatch{
					Service:   "stripe",
					Operation: "payment_methods.attach",
					AnyCall:   true,
				},
				Inject: config.ErrorInject{
					Library: "stripe.insufficient_funds",
				},
			},
		},
	}

	engine, err := NewEngineFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewEngineFromConfig() error = %v", err)
	}

	decision, err := engine.Evaluate(Request{Service: "stripe", Operation: "payment_methods.attach"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !decision.Matched {
		t.Fatal("Evaluate() did not match")
	}
	errorBody := decision.Override.Body["error"].(map[string]any)
	if errorBody["decline_code"] != "insufficient_funds" {
		t.Fatalf("decline_code = %q, want insufficient_funds", errorBody["decline_code"])
	}
}

func TestEngineRejectsUnknownNamedLibraryEntry(t *testing.T) {
	t.Parallel()

	_, err := NewEngine([]Rule{
		{
			Match: Match{
				Service:   "stripe",
				Operation: "payment_intents.create",
				AnyCall:   true,
			},
			Inject: Inject{
				Library: "stripe.not_real",
			},
		},
	})
	if err == nil {
		t.Fatal("NewEngine() expected unknown library failure")
	}
}

func TestAppendProvenancePersistsThroughRunMetadata(t *testing.T) {
	t.Parallel()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	startedAt := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(time.Second)
	run := store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_injection_001",
		SessionName:        "injection-flow",
		Mode:               recorder.RunModeReplay,
		Status:             store.RunLifecycleStatusComplete,
		StartedAt:          startedAt,
		EndedAt:            &endedAt,
	}
	run.Metadata = AppendProvenance(run.Metadata, Provenance{
		RuleIndex:  0,
		Service:    "stripe",
		Operation:  "payment_intents.create",
		CallNumber: 3,
		NthCall:    3,
		Library:    "stripe.card_declined",
		Status:     402,
	})

	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	got, err := sqliteStore.GetRunRecord(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("GetRunRecord() error = %v", err)
	}

	section := got.Metadata[MetadataKey].(map[string]any)
	applied := section["applied"].([]any)
	entry := applied[0].(map[string]any)
	if entry["library"] != "stripe.card_declined" {
		t.Fatalf("metadata library = %q, want stripe.card_declined", entry["library"])
	}
	if entry["call_number"] != float64(3) {
		t.Fatalf("metadata call_number = %#v, want 3", entry["call_number"])
	}
}
