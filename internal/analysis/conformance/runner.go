package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"stagehand/internal/services/stripe"
	"stagehand/internal/store"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Runner struct {
	httpClient   HTTPClient
	sessionStore store.SessionStore
	env          map[string]string
	now          func() time.Time
}

type Option func(*Runner)

func WithHTTPClient(client HTTPClient) Option {
	return func(r *Runner) {
		if client != nil {
			r.httpClient = client
		}
	}
}

func WithSessionStore(sessionStore store.SessionStore) Option {
	return func(r *Runner) {
		r.sessionStore = sessionStore
	}
}

func WithEnv(env map[string]string) Option {
	return func(r *Runner) {
		if env == nil {
			return
		}
		r.env = map[string]string{}
		for key, value := range env {
			r.env[key] = value
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(r *Runner) {
		if now != nil {
			r.now = now
		}
	}
}

type observation struct {
	StepID        string         `json:"step_id"`
	Operation     string         `json:"operation"`
	Request       map[string]any `json:"request,omitempty"`
	Response      map[string]any `json:"response,omitempty"`
	StatusCode    int            `json:"status_code,omitempty"`
	Error         map[string]any `json:"error,omitempty"`
	InteractionID string         `json:"interaction_id"`
}

func NewRunner(opts ...Option) *Runner {
	r := &Runner{
		httpClient: http.DefaultClient,
		env:        map[string]string{},
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Runner) RunCase(ctx context.Context, testCase Case) Result {
	now := r.now()
	result := Result{
		CaseID:  testCase.ID,
		Service: testCase.Service,
		RealRun: RunReference{
			RunID:       runReferenceID("real", testCase.ID, now),
			SessionName: sessionName(testCase.ID, "real"),
			Mode:        "record",
		},
		SimulatorRun: RunReference{
			RunID:       runReferenceID("sim", testCase.ID, now),
			SessionName: sessionName(testCase.ID, "sim"),
			Mode:        "record",
		},
	}

	if missing := r.missingRequiredCredentials(testCase); len(missing) > 0 {
		result.Status = ResultStatusSkipped
		result.Failures = append(result.Failures, Failure{
			Code:    "missing_credentials",
			Message: "missing required credentials: " + strings.Join(missing, ", "),
		})
		return result
	}

	realObservations, err := r.runReal(ctx, testCase)
	if err != nil {
		result.Status = ResultStatusError
		result.Failures = append(result.Failures, Failure{Code: "real_runner_error", Message: err.Error()})
		return result
	}
	simObservations, err := r.runSimulator(ctx, testCase, result.SimulatorRun.SessionName)
	if err != nil {
		result.Status = ResultStatusError
		result.Failures = append(result.Failures, Failure{Code: "simulator_runner_error", Message: err.Error()})
		return result
	}

	compareObservations(testCase, realObservations, simObservations, &result)
	if result.Summary.FailingDiffs > 0 || result.Summary.MissingInteractions > 0 || result.Summary.ExtraInteractions > 0 {
		result.Status = ResultStatusFailed
	} else {
		result.Status = ResultStatusPassed
	}
	return result
}

func (r *Runner) missingRequiredCredentials(testCase Case) []string {
	missing := []string{}
	for _, credential := range testCase.RealService.Credentials {
		if !credential.Required {
			continue
		}
		value := r.envValue(credential.Env)
		if strings.TrimSpace(value) == "" {
			missing = append(missing, credential.Env)
		}
	}
	sort.Strings(missing)
	return missing
}

func (r *Runner) envValue(key string) string {
	if r.env != nil {
		if value, ok := r.env[key]; ok {
			return value
		}
	}
	return os.Getenv(key)
}

func (r *Runner) runReal(ctx context.Context, testCase Case) ([]observation, error) {
	switch strings.ToLower(testCase.Service) {
	case "openai":
		return r.runOpenAIReal(ctx, testCase)
	case "stripe":
		return r.runStripeReal(ctx, testCase)
	default:
		return nil, fmt.Errorf("real-service runner for service %q is not implemented", testCase.Service)
	}
}

func (r *Runner) runSimulator(ctx context.Context, testCase Case, sessionName string) ([]observation, error) {
	switch strings.ToLower(testCase.Service) {
	case "openai":
		return r.runOpenAISimulator(testCase), nil
	case "stripe":
		return r.runStripeSimulator(ctx, testCase, sessionName)
	default:
		return nil, fmt.Errorf("simulator runner for service %q is not implemented", testCase.Service)
	}
}

func (r *Runner) runOpenAIReal(ctx context.Context, testCase Case) ([]observation, error) {
	apiKey := r.envValue("OPENAI_API_KEY")
	observations := make([]observation, 0, len(testCase.Inputs.Steps))
	resolved := map[string]observation{}
	for idx, step := range testCase.Inputs.Steps {
		if step.Operation != "chat.completions.create" && step.Operation != "responses.create" {
			return nil, fmt.Errorf("unsupported OpenAI operation %q", step.Operation)
		}
		request, err := resolveRequestTemplates(step.Request, resolved)
		if err != nil {
			return nil, fmt.Errorf("resolve OpenAI request for step %q: %w", step.ID, err)
		}
		endpoint := "https://api.openai.com/v1/chat/completions"
		if step.Operation == "responses.create" {
			endpoint = "https://api.openai.com/v1/responses"
		}
		body, err := json.Marshal(request)
		if err != nil {
			return nil, fmt.Errorf("encode OpenAI request for step %q: %w", step.ID, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("authorization", "Bearer "+apiKey)
		req.Header.Set("content-type", "application/json")
		response, err := r.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("run OpenAI step %q: %w", step.ID, err)
		}
		observed, err := decodeHTTPObservation(response)
		if err != nil {
			return nil, fmt.Errorf("decode OpenAI step %q: %w", step.ID, err)
		}
		observed.StepID = step.ID
		observed.Operation = step.Operation
		observed.Request = cloneMap(request)
		observed.InteractionID = interactionID("real", idx)
		observations = append(observations, observed)
		resolved[step.ID] = observed
	}
	return observations, nil
}

func (r *Runner) runStripeReal(ctx context.Context, testCase Case) ([]observation, error) {
	apiKey := r.envValue("STRIPE_SECRET_KEY")
	observations := make([]observation, 0, len(testCase.Inputs.Steps))
	resolved := map[string]observation{}
	for idx, step := range testCase.Inputs.Steps {
		request, err := resolveRequestTemplates(step.Request, resolved)
		if err != nil {
			return nil, fmt.Errorf("resolve Stripe request for step %q: %w", step.ID, err)
		}
		resolvedStep := step
		resolvedStep.Request = request
		method, endpoint, err := stripeEndpoint(resolvedStep)
		if err != nil {
			return nil, err
		}
		var body io.Reader
		if method == http.MethodPost {
			encoded := url.Values{}
			encodeStripeForm("", request, encoded)
			body = strings.NewReader(encoded.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(apiKey, "")
		if method == http.MethodPost {
			req.Header.Set("content-type", "application/x-www-form-urlencoded")
		}
		response, err := r.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("run Stripe step %q: %w", step.ID, err)
		}
		observed, err := decodeHTTPObservation(response)
		if err != nil {
			return nil, fmt.Errorf("decode Stripe step %q: %w", step.ID, err)
		}
		observed.StepID = step.ID
		observed.Operation = step.Operation
		observed.Request = cloneMap(request)
		observed.InteractionID = interactionID("real", idx)
		observations = append(observations, observed)
		resolved[step.ID] = observed
	}
	return observations, nil
}

func (r *Runner) runOpenAISimulator(testCase Case) []observation {
	observations := make([]observation, 0, len(testCase.Inputs.Steps))
	resolved := map[string]observation{}
	for idx, step := range testCase.Inputs.Steps {
		request, err := resolveRequestTemplates(step.Request, resolved)
		if err != nil {
			observations = append(observations, observation{
				StepID:        step.ID,
				Operation:     step.Operation,
				Request:       cloneMap(step.Request),
				Error:         map[string]any{"message": err.Error()},
				InteractionID: interactionID("sim", idx),
			})
			continue
		}
		response := map[string]any{
			"object": "chat.completion",
			"model":  stringValue(request["model"]),
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "pong",
					},
					"finish_reason": "stop",
				},
			},
		}
		if step.Operation == "responses.create" {
			response = map[string]any{
				"object": "response",
				"model":  stringValue(request["model"]),
				"output": []any{
					map[string]any{
						"type": "message",
						"role": "assistant",
						"content": []any{
							map[string]any{"type": "output_text", "text": "pong"},
						},
					},
				},
			}
		}
		observations = append(observations, observation{
			StepID:        step.ID,
			Operation:     step.Operation,
			Request:       cloneMap(request),
			Response:      map[string]any{"body": response},
			StatusCode:    200,
			InteractionID: interactionID("sim", idx),
		})
		resolved[step.ID] = observations[len(observations)-1]
	}
	return observations
}

func (r *Runner) runStripeSimulator(ctx context.Context, testCase Case, sessionName string) ([]observation, error) {
	if r.sessionStore == nil {
		return nil, fmt.Errorf("stripe simulator runner requires a session store")
	}
	sim, err := stripe.NewSimulator(r.sessionStore, stripe.WithClock(r.now))
	if err != nil {
		return nil, err
	}
	if _, err := sim.CreateSession(ctx, sessionName); err != nil {
		return nil, err
	}

	observations := make([]observation, 0, len(testCase.Inputs.Steps))
	resolved := map[string]observation{}
	for idx, step := range testCase.Inputs.Steps {
		request, err := resolveRequestTemplates(step.Request, resolved)
		if err != nil {
			return nil, fmt.Errorf("resolve Stripe simulator request for step %q: %w", step.ID, err)
		}
		resolvedStep := step
		resolvedStep.Request = request
		response, statusCode, stepErr := runStripeSimulatorStep(ctx, sim, sessionName, resolvedStep)
		observed := observation{
			StepID:        step.ID,
			Operation:     step.Operation,
			Request:       cloneMap(request),
			Response:      map[string]any{"body": response},
			StatusCode:    statusCode,
			InteractionID: interactionID("sim", idx),
		}
		if stepErr != nil {
			observed.Error = errorMap(stepErr)
		}
		observations = append(observations, observed)
		resolved[step.ID] = observed
	}
	return observations, nil
}

func runStripeSimulatorStep(ctx context.Context, sim *stripe.Simulator, sessionName string, step CaseStep) (map[string]any, int, error) {
	switch step.Operation {
	case stripe.OperationCustomersCreate:
		value, err := sim.CreateCustomer(ctx, sessionName, stripe.CreateCustomerParams{
			Email:       stringValue(step.Request["email"]),
			Name:        stringValue(step.Request["name"]),
			Description: stringValue(step.Request["description"]),
			Metadata:    stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationCustomersSearch:
		value, err := sim.SearchCustomers(ctx, sessionName, stringValue(step.Request["query"]))
		return objectMap(value), statusCode(err), err
	case stripe.OperationCustomersRetrieve:
		value, err := sim.GetCustomer(ctx, sessionName, stringValue(step.Request["id"]))
		return objectMap(value), statusCode(err), err
	case stripe.OperationCustomersUpdate:
		value, err := sim.UpdateCustomer(ctx, sessionName, stringValue(step.Request["id"]), stripe.UpdateCustomerParams{
			Email:       optionalString(step.Request, "email"),
			Name:        optionalString(step.Request, "name"),
			Description: optionalString(step.Request, "description"),
			Metadata:    stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentMethodsCreate:
		value, err := sim.CreatePaymentMethod(ctx, sessionName, stripe.CreatePaymentMethodParams{
			Type:           stringValue(step.Request["type"]),
			BillingDetails: billingDetails(step.Request["billing_details"]),
			Card:           card(step.Request["card"]),
			Metadata:       stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentMethodsRetrieve:
		value, err := sim.GetPaymentMethod(ctx, sessionName, stringValue(step.Request["id"]))
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentMethodsUpdate:
		value, err := sim.UpdatePaymentMethod(ctx, sessionName, stringValue(step.Request["id"]), stripe.UpdatePaymentMethodParams{
			BillingDetails: optionalBillingDetails(step.Request, "billing_details"),
			Card:           optionalCard(step.Request, "card"),
			Metadata:       stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentMethodsAttach:
		value, err := sim.AttachPaymentMethod(ctx, sessionName, stringValue(step.Request["id"]), stripe.AttachPaymentMethodParams{
			CustomerID: stringValue(step.Request["customer"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentIntentsCreate:
		paymentMethodID, err := simulatorPaymentMethodID(ctx, sim, sessionName, step.Request)
		if err != nil {
			return nil, statusCode(err), err
		}
		if paymentMethodID == "" {
			paymentMethodID = stringValue(step.Request["payment_method"])
		}
		status := stringValue(step.Request["status"])
		if status == "" && boolValue(step.Request["confirm"]) {
			status = stripe.PaymentIntentStatusSucceeded
		}
		value, err := sim.CreatePaymentIntent(ctx, sessionName, stripe.CreatePaymentIntentParams{
			Amount:          int64Value(step.Request["amount"]),
			Currency:        stringValue(step.Request["currency"]),
			CustomerID:      stringValue(step.Request["customer"]),
			PaymentMethodID: paymentMethodID,
			Status:          status,
			Metadata:        stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentIntentsList:
		value, err := sim.ListPaymentIntents(ctx, sessionName, stripe.ListPaymentIntentsParams{
			CustomerID: stringValue(step.Request["customer"]),
			Limit:      int(int64Value(step.Request["limit"])),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentIntentsRetrieve:
		value, err := sim.GetPaymentIntent(ctx, sessionName, stringValue(step.Request["id"]))
		return objectMap(value), statusCode(err), err
	case stripe.OperationPaymentIntentsUpdate:
		value, err := sim.UpdatePaymentIntent(ctx, sessionName, stringValue(step.Request["id"]), stripe.UpdatePaymentIntentParams{
			Amount:          optionalInt64(step.Request, "amount"),
			Currency:        optionalString(step.Request, "currency"),
			CustomerID:      optionalString(step.Request, "customer"),
			PaymentMethodID: optionalString(step.Request, "payment_method"),
			Status:          optionalString(step.Request, "status"),
			Metadata:        stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	case stripe.OperationRefundsCreate:
		value, err := sim.CreateRefund(ctx, sessionName, stripe.CreateRefundParams{
			PaymentIntentID: stringValue(step.Request["payment_intent"]),
			Amount:          int64Value(step.Request["amount"]),
			Reason:          stringValue(step.Request["reason"]),
			Metadata:        stringMap(step.Request["metadata"]),
		})
		return objectMap(value), statusCode(err), err
	default:
		return nil, 0, fmt.Errorf("unsupported Stripe simulator operation %q", step.Operation)
	}
}

func compareObservations(testCase Case, realObservations []observation, simObservations []observation, result *Result) {
	realByKey := indexObservations(realObservations, testCase.Comparison.Match)
	simByKey := indexObservations(simObservations, testCase.Comparison.Match)
	keys := sortedKeys(realByKey, simByKey)
	for _, key := range keys {
		realObserved, hasReal := realByKey[key]
		simObserved, hasSim := simByKey[key]
		switch {
		case !hasReal:
			result.Summary.ExtraInteractions++
			result.Failures = append(result.Failures, Failure{Code: "extra_interaction", Message: "simulator produced an extra interaction", SimulatorInteractionID: simObserved.InteractionID, StepID: simObserved.StepID})
		case !hasSim:
			result.Summary.MissingInteractions++
			result.Failures = append(result.Failures, Failure{Code: "missing_interaction", Message: "simulator did not produce a matching interaction", RealInteractionID: realObserved.InteractionID, StepID: realObserved.StepID})
		default:
			result.Summary.MatchedInteractions++
			tolerated := toleratedPaths(testCase.Comparison.ToleratedDiffFields, realObserved.Operation)
			diffPaths := differingPaths(observationMap(realObserved), observationMap(simObserved))
			failing := subtractPaths(diffPaths, tolerated)
			matchedTolerated := intersectPaths(diffPaths, tolerated)
			matchedTolerated = removeInternalTolerances(matchedTolerated)
			result.Summary.ToleratedDiffs += len(matchedTolerated)
			result.Matches = append(result.Matches, MatchResult{
				StepID:                 realObserved.StepID,
				RealInteractionID:      realObserved.InteractionID,
				SimulatorInteractionID: simObserved.InteractionID,
				Operation:              realObserved.Operation,
				ToleratedFields:        matchedTolerated,
			})
			for _, path := range failing {
				result.Summary.FailingDiffs++
				result.Failures = append(result.Failures, Failure{
					Code:                   "field_mismatch",
					Message:                "real and simulator values differ at " + path,
					StepID:                 realObserved.StepID,
					Path:                   path,
					RealInteractionID:      realObserved.InteractionID,
					SimulatorInteractionID: simObserved.InteractionID,
				})
			}
		}
	}
}
