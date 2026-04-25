package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/runtime/injection"
	"stagehand/internal/services/stripe"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
	"stagehand/internal/version"
)

const demoSessionName = "stripe-failure-injection-demo"

type DemoSummary struct {
	SessionName       string                 `json:"session_name"`
	RunID             string                 `json:"run_id"`
	Attempts          []PaymentIntentAttempt `json:"attempts"`
	Provenance        []injection.Provenance `json:"provenance"`
	PersistedMetadata map[string]any         `json:"persisted_metadata"`
}

type PaymentIntentAttempt struct {
	Number          int                 `json:"number"`
	Amount          int64               `json:"amount"`
	PaymentIntentID string              `json:"payment_intent_id,omitempty"`
	Status          string              `json:"status"`
	Error           *StripeErrorSummary `json:"error,omitempty"`
}

type StripeErrorSummary struct {
	StatusCode int    `json:"status_code"`
	Type       string `json:"type"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func main() {
	tempDir, err := os.MkdirTemp("", "stagehand-failure-injection-demo-*")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(tempDir)

	summary, err := RunFailureInjectionDemo(context.Background(), filepath.Join(tempDir, "stagehand.db"))
	if err != nil {
		fatal(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fatal(err)
	}
}

func RunFailureInjectionDemo(ctx context.Context, databasePath string) (DemoSummary, error) {
	sqliteStore, err := sqlitestore.OpenStore(ctx, databasePath)
	if err != nil {
		return DemoSummary{}, err
	}
	defer sqliteStore.Close()

	engine, err := injection.NewEngineFromConfig(config.ErrorInjectionConfig{
		Enabled: true,
		Rules: []config.ErrorInjectionRule{
			{
				Match: config.ErrorMatch{
					Service:   stripe.Service,
					Operation: stripe.OperationPaymentIntentsCreate,
					NthCall:   3,
				},
				Inject: config.ErrorInject{
					Library: "stripe.card_declined",
				},
			},
		},
	})
	if err != nil {
		return DemoSummary{}, err
	}

	sim, err := stripe.NewSimulator(sqliteStore, stripe.WithErrorInjection(engine))
	if err != nil {
		return DemoSummary{}, err
	}
	if _, err := sim.CreateSession(ctx, demoSessionName); err != nil {
		return DemoSummary{}, err
	}

	customer, err := sim.CreateCustomer(ctx, demoSessionName, stripe.CreateCustomerParams{
		Email: "demo-customer@example.com",
		Name:  "Failure Injection Demo",
	})
	if err != nil {
		return DemoSummary{}, err
	}

	paymentMethod, err := sim.CreatePaymentMethod(ctx, demoSessionName, stripe.CreatePaymentMethodParams{
		Type: "card",
		Card: stripe.Card{
			Brand:    "visa",
			Last4:    "4242",
			ExpMonth: 12,
			ExpYear:  2030,
		},
	})
	if err != nil {
		return DemoSummary{}, err
	}
	if _, err := sim.AttachPaymentMethod(ctx, demoSessionName, paymentMethod.ID, stripe.AttachPaymentMethodParams{
		CustomerID: customer.ID,
	}); err != nil {
		return DemoSummary{}, err
	}

	attempts := make([]PaymentIntentAttempt, 0, 4)
	for idx, amount := range []int64{1000, 2000, 3000, 4000} {
		attempt, err := createDemoPaymentIntent(ctx, sim, customer.ID, paymentMethod.ID, idx+1, amount)
		if err != nil {
			return DemoSummary{}, err
		}
		attempts = append(attempts, attempt)
	}
	if err := validateDemoAttempts(attempts); err != nil {
		return DemoSummary{}, err
	}

	runID := "run_stripe_failure_injection_demo"
	metadata := sim.ErrorInjectionMetadata(map[string]any{
		"demo": "stripe_failure_injection",
	})
	now := time.Now().UTC()
	if err := sqliteStore.CreateRun(ctx, store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         version.ArtifactVersion,
		RuntimeVersion:     version.ArtifactVersion,
		ScrubPolicyVersion: "v1",
		RunID:              runID,
		SessionName:        demoSessionName,
		Mode:               recorder.RunModeReplay,
		Status:             store.RunLifecycleStatusComplete,
		StartedAt:          now,
		EndedAt:            &now,
		Metadata:           metadata,
	}); err != nil {
		return DemoSummary{}, err
	}

	persistedRun, err := sqliteStore.GetRunRecord(ctx, runID)
	if err != nil {
		return DemoSummary{}, err
	}

	return DemoSummary{
		SessionName:       demoSessionName,
		RunID:             runID,
		Attempts:          attempts,
		Provenance:        sim.AppliedErrorInjections(),
		PersistedMetadata: persistedRun.Metadata,
	}, nil
}

func createDemoPaymentIntent(
	ctx context.Context,
	sim *stripe.Simulator,
	customerID string,
	paymentMethodID string,
	attemptNumber int,
	amount int64,
) (PaymentIntentAttempt, error) {
	intent, err := sim.CreatePaymentIntent(ctx, demoSessionName, stripe.CreatePaymentIntentParams{
		Amount:          amount,
		Currency:        "usd",
		CustomerID:      customerID,
		PaymentMethodID: paymentMethodID,
	})
	if err == nil {
		return PaymentIntentAttempt{
			Number:          attemptNumber,
			Amount:          amount,
			PaymentIntentID: intent.ID,
			Status:          intent.Status,
		}, nil
	}

	if !errors.Is(err, stripe.ErrInjected) {
		return PaymentIntentAttempt{}, err
	}

	var stripeErr *stripe.Error
	if !errors.As(err, &stripeErr) {
		return PaymentIntentAttempt{}, err
	}
	return PaymentIntentAttempt{
		Number: attemptNumber,
		Amount: amount,
		Status: "injected_error",
		Error: &StripeErrorSummary{
			StatusCode: stripeErr.StatusCode,
			Type:       stripeErr.Type,
			Code:       stripeErr.Code,
			Message:    stripeErr.Message,
		},
	}, nil
}

func validateDemoAttempts(attempts []PaymentIntentAttempt) error {
	if len(attempts) != 4 {
		return fmt.Errorf("expected 4 demo attempts, got %d", len(attempts))
	}
	for _, idx := range []int{0, 1, 3} {
		if attempts[idx].Error != nil {
			return fmt.Errorf("attempt %d unexpectedly failed: %#v", attempts[idx].Number, attempts[idx].Error)
		}
	}
	if attempts[2].Error == nil {
		return fmt.Errorf("attempt 3 did not inject a failure")
	}
	if attempts[2].Error.StatusCode != 402 || attempts[2].Error.Code != "card_declined" {
		return fmt.Errorf("attempt 3 error = %#v, want 402 card_declined", attempts[2].Error)
	}
	if attempts[3].PaymentIntentID != "pi_000003" {
		return fmt.Errorf("attempt 4 payment intent ID = %q, want pi_000003", attempts[3].PaymentIntentID)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
