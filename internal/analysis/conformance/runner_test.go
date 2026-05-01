package conformance

import (
	"context"
	"io"
	"net/http"
	"net/url"
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

func TestStripeRefundSmokeCaseFileValidates(t *testing.T) {
	t.Parallel()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "conformance", "stripe-refund-smoke.yml"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
}

func TestStripeRefundSmokeSkipsWithoutStripeCredential(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "conformance", "stripe-refund-smoke.yml"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%q) error = %v", path, err)
	}
	runner := NewRunner(WithEnv(map[string]string{}))
	result := runner.RunCase(context.Background(), file.Cases[0])
	if result.Status != ResultStatusSkipped {
		t.Fatalf("Status = %q, want skipped", result.Status)
	}
	if len(result.Failures) != 1 || result.Failures[0].Code != "missing_credentials" {
		t.Fatalf("Failures = %#v, want missing_credentials", result.Failures)
	}
}

func TestRunnerOpenAISmokeToleratesSurfaceTextVariation(t *testing.T) {
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
				{Path: "response.body.choices[0].message.content", Reason: "model surface text varies"},
			},
		},
	})

	if result.Status != ResultStatusPassed {
		t.Fatalf("Status = %q, want passed with content tolerance; failures=%#v", result.Status, result.Failures)
	}
	if result.Summary.ToleratedDiffs != 1 {
		t.Fatalf("ToleratedDiffs = %d, want content mismatch to be tolerated", result.Summary.ToleratedDiffs)
	}
}

func TestRunnerInterpolatesStripeRefundStepOutputs(t *testing.T) {
	store, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	calls := 0
	runner := NewRunner(
		WithEnv(map[string]string{"STRIPE_SECRET_KEY": "sk_test_refund"}),
		WithSessionStore(store),
		WithClock(fixedClock),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			body := readFormBody(t, req)
			switch calls {
			case 1:
				if req.URL.Path != "/v1/customers" {
					t.Fatalf("step 1 path = %q, want /v1/customers", req.URL.Path)
				}
				return jsonResponse(200, `{"id":"cus_real_123","object":"customer","email":"refund@example.com","created":111,"metadata":{"stagehand_case":"stripe_refund_smoke"}}`), nil
			case 2:
				if req.URL.Path != "/v1/payment_intents" {
					t.Fatalf("step 2 path = %q, want /v1/payment_intents", req.URL.Path)
				}
				if body.Get("customer") != "cus_real_123" {
					t.Fatalf("payment intent customer = %q, want interpolated real customer ID", body.Get("customer"))
				}
				if body.Get("confirm") != "true" {
					t.Fatalf("confirm = %q, want true", body.Get("confirm"))
				}
				return jsonResponse(200, `{"id":"pi_real_123","object":"payment_intent","amount":1000,"currency":"usd","customer":"cus_real_123","status":"succeeded","created":112,"client_secret":"pi_real_123_secret","payment_method":"pm_real_123","latest_charge":"ch_real_123","metadata":{"stagehand_case":"stripe_refund_smoke"}}`), nil
			case 3:
				if req.URL.Path != "/v1/payment_intents" || req.Method != http.MethodGet {
					t.Fatalf("step 3 = %s %s, want GET /v1/payment_intents", req.Method, req.URL.Path)
				}
				if req.URL.Query().Get("customer") != "cus_real_123" {
					t.Fatalf("list customer = %q, want interpolated real customer ID", req.URL.Query().Get("customer"))
				}
				return jsonResponse(200, `{"object":"list","data":[{"id":"pi_real_123","object":"payment_intent","amount":1000,"currency":"usd","customer":"cus_real_123","status":"succeeded","created":112,"client_secret":"pi_real_123_secret","payment_method":"pm_real_123","latest_charge":"ch_real_123","metadata":{"stagehand_case":"stripe_refund_smoke"}}],"has_more":false,"url":"/v1/payment_intents"}`), nil
			case 4:
				if req.URL.Path != "/v1/refunds" {
					t.Fatalf("step 4 path = %q, want /v1/refunds", req.URL.Path)
				}
				if body.Get("payment_intent") != "pi_real_123" {
					t.Fatalf("refund payment_intent = %q, want interpolated listed payment intent", body.Get("payment_intent"))
				}
				return jsonResponse(200, `{"id":"re_real_123","object":"refund","amount":1000,"currency":"usd","payment_intent":"pi_real_123","reason":"requested_by_customer","status":"succeeded","created":113,"metadata":{"stagehand_case":"stripe_refund_smoke"}}`), nil
			default:
				t.Fatalf("unexpected Stripe call %d: %s %s", calls, req.Method, req.URL.String())
			}
			return nil, nil
		})),
	)

	result := runner.RunCase(context.Background(), stripeRefundCaseForTest([]ToleratedDiffField{
		{Path: "request.customer", Reason: "ids differ"},
		{Path: "request.payment_intent", Reason: "ids differ"},
		{Path: "response.body.id", Reason: "ids differ"},
		{Path: "response.body.created", Reason: "timestamps differ"},
		{Path: "response.body.customer", Reason: "ids differ"},
		{Path: "response.body.client_secret", Reason: "secrets differ"},
		{Path: "response.body.payment_method", Reason: "ids differ"},
		{Path: "response.body.latest_charge", Reason: "ids differ"},
		{Path: "response.body.data[0].id", Reason: "ids differ"},
		{Path: "response.body.data[0].created", Reason: "timestamps differ"},
		{Path: "response.body.data[0].customer", Reason: "ids differ"},
		{Path: "response.body.data[0].client_secret", Reason: "secrets differ"},
		{Path: "response.body.data[0].payment_method", Reason: "ids differ"},
		{Path: "response.body.data[0].latest_charge", Reason: "ids differ"},
		{Path: "response.body.payment_intent", Reason: "ids differ"},
	}))

	if calls != 4 {
		t.Fatalf("Stripe calls = %d, want 4", calls)
	}
	if result.Status != ResultStatusPassed {
		t.Fatalf("Status = %q, want passed; failures=%#v", result.Status, result.Failures)
	}
	if result.Summary.MatchedInteractions != 4 {
		t.Fatalf("MatchedInteractions = %d, want 4", result.Summary.MatchedInteractions)
	}
}

func TestRunnerStripeRefundSmokeDetectsUntoleratedRefundDrift(t *testing.T) {
	store, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	calls := 0
	runner := NewRunner(
		WithEnv(map[string]string{"STRIPE_SECRET_KEY": "sk_test_refund"}),
		WithSessionStore(store),
		WithClock(fixedClock),
		WithHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return jsonResponse(200, `{"id":"cus_real_123","object":"customer","email":"refund@example.com","created":111,"metadata":{"stagehand_case":"stripe_refund_smoke"}}`), nil
			case 2:
				return jsonResponse(200, `{"id":"pi_real_123","object":"payment_intent","amount":1000,"currency":"usd","customer":"cus_real_123","status":"succeeded","created":112,"client_secret":"pi_real_123_secret","payment_method":"pm_real_123","latest_charge":"ch_real_123","metadata":{"stagehand_case":"stripe_refund_smoke"}}`), nil
			case 3:
				return jsonResponse(200, `{"object":"list","data":[{"id":"pi_real_123","object":"payment_intent","amount":1000,"currency":"usd","customer":"cus_real_123","status":"succeeded","created":112,"client_secret":"pi_real_123_secret","payment_method":"pm_real_123","latest_charge":"ch_real_123","metadata":{"stagehand_case":"stripe_refund_smoke"}}]}`), nil
			case 4:
				return jsonResponse(200, `{"id":"re_real_123","object":"refund","amount":1000,"currency":"usd","payment_intent":"pi_real_123","reason":"requested_by_customer","status":"pending","created":113,"metadata":{"stagehand_case":"stripe_refund_smoke"}}`), nil
			default:
				t.Fatalf("unexpected Stripe call %d", calls)
			}
			return nil, nil
		})),
	)

	result := runner.RunCase(context.Background(), stripeRefundCaseForTest([]ToleratedDiffField{
		{Path: "request.customer", Reason: "ids differ"},
		{Path: "request.payment_intent", Reason: "ids differ"},
		{Path: "response.body.id", Reason: "ids differ"},
		{Path: "response.body.created", Reason: "timestamps differ"},
		{Path: "response.body.customer", Reason: "ids differ"},
		{Path: "response.body.client_secret", Reason: "secrets differ"},
		{Path: "response.body.payment_method", Reason: "ids differ"},
		{Path: "response.body.latest_charge", Reason: "ids differ"},
		{Path: "response.body.data[0].id", Reason: "ids differ"},
		{Path: "response.body.data[0].created", Reason: "timestamps differ"},
		{Path: "response.body.data[0].customer", Reason: "ids differ"},
		{Path: "response.body.data[0].client_secret", Reason: "secrets differ"},
		{Path: "response.body.data[0].payment_method", Reason: "ids differ"},
		{Path: "response.body.data[0].latest_charge", Reason: "ids differ"},
		{Path: "response.body.payment_intent", Reason: "ids differ"},
	}))

	if result.Status != ResultStatusFailed {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	found := false
	for _, failure := range result.Failures {
		if failure.StepID == "refund-payment" && failure.Path == "response.body.status" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Failures = %#v, want refund response.body.status drift", result.Failures)
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

func TestWithEnvNilPreservesProcessEnvironmentFallback(t *testing.T) {
	t.Setenv("STAGEHAND_CONFORMANCE_TEST_ENV", "present")
	runner := NewRunner(WithEnv(nil))
	if got := runner.envValue("STAGEHAND_CONFORMANCE_TEST_ENV"); got != "present" {
		t.Fatalf("envValue() = %q, want process environment fallback", got)
	}
}

func TestRunCaseUsesSingleTimestampForRunReferences(t *testing.T) {
	calls := 0
	runner := NewRunner(
		WithEnv(map[string]string{}),
		WithClock(func() time.Time {
			calls++
			return time.Date(2026, time.April, 28, 12, 0, 0, calls, time.UTC)
		}),
	)
	result := runner.RunCase(context.Background(), Case{
		ID:      "timestamp-case",
		Service: "openai",
		Inputs:  CaseInputs{Steps: []CaseStep{{ID: "chat", Operation: "chat.completions.create"}}},
		RealService: RealServiceRequirements{
			Credentials: []CredentialRequirement{{Env: "OPENAI_API_KEY", Required: true, Purpose: "OpenAI test key"}},
		},
		Comparison: ComparisonConfig{Match: MatchConfig{Strategy: MatchStrategyOperationSequence}},
	})
	realSuffix := result.RealRun.RunID[strings.LastIndex(result.RealRun.RunID, "_")+1:]
	simSuffix := result.SimulatorRun.RunID[strings.LastIndex(result.SimulatorRun.RunID, "_")+1:]
	if realSuffix != simSuffix {
		t.Fatalf("run reference timestamp suffixes differ: real=%s sim=%s", realSuffix, simSuffix)
	}
}

func TestDecodeHTTPObservationIncludesHTTPStatusForNonJSONErrors(t *testing.T) {
	observed, err := decodeHTTPObservation(&http.Response{
		Status:     "500 Internal Server Error",
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("")),
	})
	if err != nil {
		t.Fatalf("decodeHTTPObservation() error = %v", err)
	}
	if observed.Error["status"] != "500 Internal Server Error" {
		t.Fatalf("Error status = %#v, want HTTP status text", observed.Error)
	}
	if observed.Error["status_code"] != 500 {
		t.Fatalf("Error status_code = %#v, want 500", observed.Error)
	}
}

func TestEncodeStripeFormUsesUnindexedArrayBrackets(t *testing.T) {
	values := url.Values{}
	encodeStripeForm("", map[string]any{"expand": []any{"customer", "payment_method"}}, values)
	if got := values["expand[]"]; len(got) != 2 || got[0] != "customer" || got[1] != "payment_method" {
		t.Fatalf("expand[] = %#v, want unindexed Stripe array form fields", got)
	}
	if _, ok := values["expand[0]"]; ok {
		t.Fatalf("unexpected indexed array key: %#v", values)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"content-type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func readFormBody(t *testing.T, req *http.Request) url.Values {
	t.Helper()
	if req.Body == nil {
		return url.Values{}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", string(body), err)
	}
	return values
}

func stripeRefundCaseForTest(tolerated []ToleratedDiffField) Case {
	return Case{
		ID:      "stripe-refund-smoke",
		Service: "stripe",
		Inputs: CaseInputs{Steps: []CaseStep{
			{
				ID:        "create-customer",
				Operation: "customers.create",
				Request: map[string]any{
					"email": "refund@example.com",
					"metadata": map[string]any{
						"stagehand_case": "stripe_refund_smoke",
					},
				},
			},
			{
				ID:        "create-payment-intent",
				Operation: "payment_intents.create",
				Request: map[string]any{
					"amount":   1000,
					"currency": "usd",
					"customer": "{{ create-customer.response.body.id }}",
					"confirm":  true,
					"payment_method_data": map[string]any{
						"type": "card",
						"card": map[string]any{
							"token": "tok_visa",
						},
					},
					"metadata": map[string]any{
						"stagehand_case": "stripe_refund_smoke",
					},
				},
			},
			{
				ID:        "list-payment-intents",
				Operation: "payment_intents.list",
				Request: map[string]any{
					"customer": "{{ create-customer.response.body.id }}",
					"limit":    1,
				},
			},
			{
				ID:        "refund-payment",
				Operation: "refunds.create",
				Request: map[string]any{
					"payment_intent": "{{ list-payment-intents.response.body.data[0].id }}",
					"reason":         "requested_by_customer",
					"metadata": map[string]any{
						"stagehand_case": "stripe_refund_smoke",
					},
				},
			},
		}},
		RealService: RealServiceRequirements{
			TestMode: true,
			Credentials: []CredentialRequirement{
				{Env: "STRIPE_SECRET_KEY", Required: true, Purpose: "Stripe test-mode key"},
			},
		},
		Comparison: ComparisonConfig{
			Match:               MatchConfig{Strategy: MatchStrategyOperationSequence},
			ToleratedDiffFields: tolerated,
		},
	}
}

func fixedClock() time.Time {
	return time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
}
