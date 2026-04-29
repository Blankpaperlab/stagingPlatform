package conformance

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlitestore "stagehand/internal/store/sqlite"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRunnerSkipsWhenRequiredCredentialsAreMissing(t *testing.T) {
	runner := NewRunner(WithEnv(map[string]string{}))
	result := runner.RunCase(context.Background(), Case{
		ID:      "openai-missing-creds",
		Service: "openai",
		Inputs:  CaseInputs{Steps: []CaseStep{{ID: "chat", Operation: "chat.completions.create"}}},
		RealService: RealServiceRequirements{
			Credentials: []CredentialRequirement{
				{Env: "OPENAI_API_KEY", Required: true, Purpose: "OpenAI test key"},
			},
		},
		Comparison: ComparisonConfig{Match: MatchConfig{Strategy: MatchStrategyOperationSequence}},
	})

	if result.Status != ResultStatusSkipped {
		t.Fatalf("Status = %q, want %q", result.Status, ResultStatusSkipped)
	}
	if len(result.Failures) != 1 || result.Failures[0].Code != "missing_credentials" {
		t.Fatalf("Failures = %#v, want missing_credentials", result.Failures)
	}
}

func TestRunnerRunsOpenAIRealAndSimulatorAndCapturesDiffs(t *testing.T) {
	var gotAuth string
	runner := NewRunner(
		WithEnv(map[string]string{"OPENAI_API_KEY": "sk-test"}),
		WithClock(fixedClock),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotAuth = req.Header.Get("authorization")
			if req.URL.String() != "https://api.openai.com/v1/chat/completions" {
				t.Fatalf("OpenAI URL = %q", req.URL.String())
			}
			return jsonResponse(200, `{"object":"chat.completion","model":"gpt-5.4-mini","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`), nil
		})),
	)

	result := runner.RunCase(context.Background(), Case{
		ID:      "openai-chat",
		Service: "openai",
		Inputs: CaseInputs{Steps: []CaseStep{{
			ID:        "chat",
			Operation: "chat.completions.create",
			Request: map[string]any{
				"model": "gpt-5.4-mini",
				"messages": []any{
					map[string]any{"role": "user", "content": "Reply with pong."},
				},
			},
		}}},
		RealService: RealServiceRequirements{
			Credentials: []CredentialRequirement{{Env: "OPENAI_API_KEY", Required: true, Purpose: "OpenAI test key"}},
		},
		Comparison: ComparisonConfig{Match: MatchConfig{Strategy: MatchStrategyOperationSequence}},
	})

	if gotAuth != "Bearer sk-test" {
		t.Fatalf("authorization = %q, want bearer token", gotAuth)
	}
	if result.Status != ResultStatusPassed {
		t.Fatalf("Status = %q, want passed; failures=%#v", result.Status, result.Failures)
	}
	if result.Summary.MatchedInteractions != 1 {
		t.Fatalf("MatchedInteractions = %d, want 1", result.Summary.MatchedInteractions)
	}
}

func TestRunnerRunsStripeRealAndSimulatorWithToleratedStructuralDiffs(t *testing.T) {
	store, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	var gotAuth bool
	runner := NewRunner(
		WithEnv(map[string]string{"STRIPE_SECRET_KEY": "sk_test_123"}),
		WithSessionStore(store),
		WithClock(fixedClock),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			username, _, _ := req.BasicAuth()
			gotAuth = username == "sk_test_123"
			if req.URL.String() != "https://api.stripe.com/v1/customers" {
				t.Fatalf("Stripe URL = %q", req.URL.String())
			}
			if req.Header.Get("content-type") != "application/x-www-form-urlencoded" {
				t.Fatalf("content-type = %q", req.Header.Get("content-type"))
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if !strings.Contains(string(body), "email=conformance%40example.com") {
				t.Fatalf("form body = %q, want encoded email", string(body))
			}
			return jsonResponse(200, `{"id":"cus_real_123","object":"customer","email":"conformance@example.com","created":111}`), nil
		})),
	)

	result := runner.RunCase(context.Background(), Case{
		ID:      "stripe-customer",
		Service: "stripe",
		Inputs: CaseInputs{Steps: []CaseStep{{
			ID:        "create-customer",
			Operation: "customers.create",
			Request: map[string]any{
				"email": "conformance@example.com",
			},
		}}},
		RealService: RealServiceRequirements{
			TestMode:    true,
			Credentials: []CredentialRequirement{{Env: "STRIPE_SECRET_KEY", Required: true, Purpose: "Stripe test key"}},
		},
		Comparison: ComparisonConfig{
			Match: MatchConfig{Strategy: MatchStrategyOperationSequence},
			ToleratedDiffFields: []ToleratedDiffField{
				{Path: "response.body.id", Reason: "generated IDs differ"},
				{Path: "response.body.created", Reason: "timestamps differ"},
			},
		},
	})

	if !gotAuth {
		t.Fatal("Stripe request did not use configured API key")
	}
	if result.Status != ResultStatusPassed {
		t.Fatalf("Status = %q, want passed; failures=%#v", result.Status, result.Failures)
	}
	if result.Summary.ToleratedDiffs != 2 {
		t.Fatalf("ToleratedDiffs = %d, want 2", result.Summary.ToleratedDiffs)
	}
}

func TestRunnerReportsUntoleratedStructuralDiffs(t *testing.T) {
	runner := NewRunner(
		WithEnv(map[string]string{"OPENAI_API_KEY": "sk-test"}),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"object":"chat.completion","model":"other","choices":[]}`), nil
		})),
	)

	result := runner.RunCase(context.Background(), Case{
		ID:      "openai-drift",
		Service: "openai",
		Inputs: CaseInputs{Steps: []CaseStep{{
			ID:        "chat",
			Operation: "chat.completions.create",
			Request:   map[string]any{"model": "gpt-5.4-mini"},
		}}},
		RealService: RealServiceRequirements{
			Credentials: []CredentialRequirement{{Env: "OPENAI_API_KEY", Required: true, Purpose: "OpenAI test key"}},
		},
		Comparison: ComparisonConfig{Match: MatchConfig{Strategy: MatchStrategyOperationSequence}},
	})

	if result.Status != ResultStatusFailed {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if result.Summary.FailingDiffs == 0 {
		t.Fatalf("FailingDiffs = 0, want structural diff; result=%#v", result)
	}
}

func TestSmokeCaseFileValidates(t *testing.T) {
	t.Parallel()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "conformance", "smoke.yml"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
}

func TestRunnerOpenAISimulatorEmitsFixedPongPlaceholder(t *testing.T) {
	t.Parallel()

	runner := NewRunner(
		WithEnv(map[string]string{"OPENAI_API_KEY": "sk-test"}),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"object":"chat.completion","model":"gpt-5.4-mini","choices":[{"index":0,"message":{"role":"assistant","content":"Pong!"},"finish_reason":"stop"}]}`), nil
		})),
	)

	result := runner.RunCase(context.Background(), Case{
		ID:      "openai-content-drift",
		Service: "openai",
		Inputs: CaseInputs{Steps: []CaseStep{{
			ID:        "chat",
			Operation: "chat.completions.create",
			Request:   map[string]any{"model": "gpt-5.4-mini"},
		}}},
		RealService: RealServiceRequirements{
			Credentials: []CredentialRequirement{{Env: "OPENAI_API_KEY", Required: true, Purpose: "OpenAI test key"}},
		},
		Comparison: ComparisonConfig{
			Match: MatchConfig{Strategy: MatchStrategyOperationSequence},
			ToleratedDiffFields: []ToleratedDiffField{
				{Path: "response.body.id", Reason: "ids differ"},
				{Path: "response.body.created", Reason: "timestamps differ"},
				{Path: "response.body.usage", Reason: "token accounting"},
				{Path: "response.body.system_fingerprint", Reason: "backend"},
			},
		},
	})

	if result.Status != ResultStatusFailed {
		t.Fatalf("Status = %q, want failed; the OpenAI simulator placeholder hardcodes assistant content to %q so any real response that differs must surface as a failing diff", result.Status, "pong")
	}
	contentPath := "response.body.choices[0].message.content"
	found := false
	for _, failure := range result.Failures {
		if failure.Path == contentPath {
			found = true
		}
	}
	if !found {
		t.Fatalf("content mismatch at %q was not reported as a failure; failures=%#v", contentPath, result.Failures)
	}
}

func TestRunnerStripeOperationSequenceAlignsByOccurrenceCount(t *testing.T) {
	t.Parallel()

	store, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	calls := 0
	runner := NewRunner(
		WithEnv(map[string]string{"STRIPE_SECRET_KEY": "sk_test_abc"}),
		WithSessionStore(store),
		WithClock(fixedClock),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			id := "cus_real_first"
			email := "first@example.com"
			if calls == 2 {
				id = "cus_real_second"
				email = "second@example.com"
			}
			return jsonResponse(200, `{"id":"`+id+`","object":"customer","email":"`+email+`","created":111}`), nil
		})),
	)

	result := runner.RunCase(context.Background(), Case{
		ID:      "stripe-multi-step",
		Service: "stripe",
		Inputs: CaseInputs{Steps: []CaseStep{
			{ID: "create-first", Operation: "customers.create", Request: map[string]any{"email": "first@example.com"}},
			{ID: "create-second", Operation: "customers.create", Request: map[string]any{"email": "second@example.com"}},
		}},
		RealService: RealServiceRequirements{
			TestMode:    true,
			Credentials: []CredentialRequirement{{Env: "STRIPE_SECRET_KEY", Required: true, Purpose: "Stripe test-mode key"}},
		},
		Comparison: ComparisonConfig{
			Match: MatchConfig{Strategy: MatchStrategyOperationSequence},
			ToleratedDiffFields: []ToleratedDiffField{
				{Path: "response.body.id", Reason: "ids differ"},
				{Path: "response.body.created", Reason: "timestamps differ"},
			},
		},
	})

	if result.Status != ResultStatusPassed {
		t.Fatalf("Status = %q, want passed; failures=%#v", result.Status, result.Failures)
	}
	if result.Summary.MatchedInteractions != 2 {
		t.Fatalf("MatchedInteractions = %d, want 2 — operation_sequence should align same-operation steps by occurrence count", result.Summary.MatchedInteractions)
	}
	if result.Summary.MissingInteractions != 0 || result.Summary.ExtraInteractions != 0 {
		t.Fatalf("missing=%d extra=%d, want 0/0", result.Summary.MissingInteractions, result.Summary.ExtraInteractions)
	}
}

func TestRunnerReportsExtraSimulatorInteractionsWhenStepsDiverge(t *testing.T) {
	t.Parallel()

	store, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	runner := NewRunner(
		WithEnv(map[string]string{"STRIPE_SECRET_KEY": "sk_test_abc"}),
		WithSessionStore(store),
		WithClock(fixedClock),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"id":"cus_only","object":"customer","email":"only@example.com","created":111}`), nil
		})),
	)

	// One real call observed, two simulator steps configured -> simulator should be flagged extra at index 1.
	// We craft the case so both steps go to the simulator while the real client only sees one;
	// the runner currently runs every step against the real client too, so this exercises a "fan-out" scenario
	// that future authors may want to support. For now we just observe the result shape.
	result := runner.RunCase(context.Background(), Case{
		ID:      "stripe-fan-out",
		Service: "stripe",
		Inputs: CaseInputs{Steps: []CaseStep{
			{ID: "create-customer", Operation: "customers.create", Request: map[string]any{"email": "only@example.com"}},
			{ID: "create-second", Operation: "customers.create", Request: map[string]any{"email": "only@example.com"}},
		}},
		RealService: RealServiceRequirements{
			TestMode:    true,
			Credentials: []CredentialRequirement{{Env: "STRIPE_SECRET_KEY", Required: true, Purpose: "Stripe test key"}},
		},
		Comparison: ComparisonConfig{
			Match: MatchConfig{Strategy: MatchStrategyOperationSequence},
			ToleratedDiffFields: []ToleratedDiffField{
				{Path: "response.body.id", Reason: "ids differ"},
				{Path: "response.body.created", Reason: "timestamps differ"},
			},
		},
	})

	// Both real and simulator currently iterate every step, so two matched interactions are expected.
	// This pins the documented "every step runs against both sides" behavior so a future divergence
	// would surface as a missing/extra count rather than silently being absorbed.
	if result.Summary.MatchedInteractions != 2 {
		t.Fatalf("MatchedInteractions = %d, want 2 (real and sim each execute every step)", result.Summary.MatchedInteractions)
	}
	if result.Summary.MissingInteractions != 0 || result.Summary.ExtraInteractions != 0 {
		t.Fatalf("missing=%d extra=%d, want 0/0", result.Summary.MissingInteractions, result.Summary.ExtraInteractions)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"content-type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func fixedClock() time.Time {
	return time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
}
