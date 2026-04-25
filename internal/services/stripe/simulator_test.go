package stripe

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"stagehand/internal/runtime/injection"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestMatchRequestRoutesSupportedStripeSubset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		method    string
		url       string
		operation string
		paramKey  string
		paramWant string
	}{
		{
			name:      "create customer",
			method:    "POST",
			url:       "https://api.stripe.com/v1/customers",
			operation: OperationCustomersCreate,
		},
		{
			name:      "retrieve customer",
			method:    "GET",
			url:       "https://api.stripe.com/v1/customers/cus_123",
			operation: OperationCustomersRetrieve,
			paramKey:  "customer_id",
			paramWant: "cus_123",
		},
		{
			name:      "update customer",
			method:    "POST",
			url:       "/v1/customers/cus_123",
			operation: OperationCustomersUpdate,
			paramKey:  "customer_id",
			paramWant: "cus_123",
		},
		{
			name:      "create payment method",
			method:    "POST",
			url:       "/v1/payment_methods",
			operation: OperationPaymentMethodsCreate,
		},
		{
			name:      "retrieve payment method",
			method:    "GET",
			url:       "/v1/payment_methods/pm_123",
			operation: OperationPaymentMethodsRetrieve,
			paramKey:  "payment_method_id",
			paramWant: "pm_123",
		},
		{
			name:      "update payment method",
			method:    "POST",
			url:       "/v1/payment_methods/pm_123",
			operation: OperationPaymentMethodsUpdate,
			paramKey:  "payment_method_id",
			paramWant: "pm_123",
		},
		{
			name:      "attach payment method",
			method:    "POST",
			url:       "/v1/payment_methods/pm_123/attach",
			operation: OperationPaymentMethodsAttach,
			paramKey:  "payment_method_id",
			paramWant: "pm_123",
		},
		{
			name:      "create payment intent",
			method:    "POST",
			url:       "/v1/payment_intents",
			operation: OperationPaymentIntentsCreate,
		},
		{
			name:      "retrieve payment intent",
			method:    "GET",
			url:       "/v1/payment_intents/pi_123",
			operation: OperationPaymentIntentsRetrieve,
			paramKey:  "payment_intent_id",
			paramWant: "pi_123",
		},
		{
			name:      "update payment intent",
			method:    "POST",
			url:       "/v1/payment_intents/pi_123",
			operation: OperationPaymentIntentsUpdate,
			paramKey:  "payment_intent_id",
			paramWant: "pi_123",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			match, ok := MatchRequest(test.method, test.url)
			if !ok {
				t.Fatalf("MatchRequest(%q, %q) did not match", test.method, test.url)
			}
			if match.Service != Service {
				t.Fatalf("Service = %q, want %q", match.Service, Service)
			}
			if match.Operation != test.operation {
				t.Fatalf("Operation = %q, want %q", match.Operation, test.operation)
			}
			if test.paramKey != "" && match.Params[test.paramKey] != test.paramWant {
				t.Fatalf("Params[%q] = %q, want %q", test.paramKey, match.Params[test.paramKey], test.paramWant)
			}
		})
	}
}

func TestMatchRequestRejectsUnsupportedStripeRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		url    string
	}{
		{method: "POST", url: "/v1/charges"},
		{method: "DELETE", url: "/v1/customers/cus_123"},
		{method: "GET", url: "/v1/payment_methods"},
		{method: "POST", url: "/v2/customers"},
	}

	for _, test := range tests {
		if match, ok := MatchRequest(test.method, test.url); ok {
			t.Fatalf("MatchRequest(%q, %q) = %#v, want no match", test.method, test.url, match)
		}
	}
}

func TestSimulatorCustomerCreateReadUpdatePersistsBySession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "stripe-onboarding"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	customer, err := sim.CreateCustomer(ctx, "stripe-onboarding", CreateCustomerParams{
		Email: "customer@example.com",
		Name:  "Ada Lovelace",
		Metadata: map[string]string{
			"source": "test",
		},
	})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	if customer.ID != "cus_000001" {
		t.Fatalf("Customer.ID = %q, want cus_000001", customer.ID)
	}
	if customer.Object != "customer" {
		t.Fatalf("Customer.Object = %q, want customer", customer.Object)
	}

	updatedName := "Ada Byron"
	updated, err := sim.UpdateCustomer(ctx, "stripe-onboarding", customer.ID, UpdateCustomerParams{
		Name: &updatedName,
		Metadata: map[string]string{
			"tier": "enterprise",
		},
	})
	if err != nil {
		t.Fatalf("UpdateCustomer() error = %v", err)
	}
	if updated.Name != updatedName {
		t.Fatalf("updated.Name = %q, want %q", updated.Name, updatedName)
	}
	if updated.Metadata["tier"] != "enterprise" {
		t.Fatalf("updated.Metadata[tier] = %q, want enterprise", updated.Metadata["tier"])
	}

	reloadedSim := newStripeTestSimulator(t, sqliteStore)
	reloaded, err := reloadedSim.GetCustomer(ctx, "stripe-onboarding", customer.ID)
	if err != nil {
		t.Fatalf("GetCustomer(reloaded) error = %v", err)
	}
	if reloaded.Name != updatedName || reloaded.Email != customer.Email {
		t.Fatalf("reloaded customer = %#v, want updated name and original email", reloaded)
	}
}

func TestSimulatorPaymentMethodAttachAndSessionIsolation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "session-a"); err != nil {
		t.Fatalf("CreateSession(session-a) error = %v", err)
	}
	if _, err := sim.CreateSession(ctx, "session-b"); err != nil {
		t.Fatalf("CreateSession(session-b) error = %v", err)
	}

	customer, err := sim.CreateCustomer(ctx, "session-a", CreateCustomerParams{
		Email: "billing@example.com",
	})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}

	paymentMethod, err := sim.CreatePaymentMethod(ctx, "session-a", CreatePaymentMethodParams{
		BillingDetails: BillingDetails{Name: "Billing User"},
		Card:           Card{Brand: "visa", Last4: "4242", ExpMonth: 12, ExpYear: 2030},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}
	if paymentMethod.ID != "pm_000001" {
		t.Fatalf("PaymentMethod.ID = %q, want pm_000001", paymentMethod.ID)
	}

	attached, err := sim.AttachPaymentMethod(ctx, "session-a", paymentMethod.ID, AttachPaymentMethodParams{
		CustomerID: customer.ID,
	})
	if err != nil {
		t.Fatalf("AttachPaymentMethod() error = %v", err)
	}
	if attached.CustomerID != customer.ID {
		t.Fatalf("attached.CustomerID = %q, want %q", attached.CustomerID, customer.ID)
	}

	if _, err := sim.GetPaymentMethod(ctx, "session-b", paymentMethod.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPaymentMethod(session-b) error = %v, want ErrNotFound", err)
	}
}

func TestSimulatorPaymentIntentCreateReadUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "payments"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	customer, err := sim.CreateCustomer(ctx, "payments", CreateCustomerParams{Name: "Payment Customer"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	paymentMethod, err := sim.CreatePaymentMethod(ctx, "payments", CreatePaymentMethodParams{
		Card: Card{Brand: "visa", Last4: "4242"},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}

	intent, err := sim.CreatePaymentIntent(ctx, "payments", CreatePaymentIntentParams{
		Amount:          2500,
		Currency:        "USD",
		CustomerID:      customer.ID,
		PaymentMethodID: paymentMethod.ID,
		Metadata: map[string]string{
			"order_id": "ord_123",
		},
	})
	if err != nil {
		t.Fatalf("CreatePaymentIntent() error = %v", err)
	}
	if intent.ID != "pi_000001" {
		t.Fatalf("PaymentIntent.ID = %q, want pi_000001", intent.ID)
	}
	if intent.Currency != "usd" {
		t.Fatalf("PaymentIntent.Currency = %q, want usd", intent.Currency)
	}
	if intent.Status != "requires_confirmation" {
		t.Fatalf("PaymentIntent.Status = %q, want requires_confirmation", intent.Status)
	}
	if intent.ClientSecret == "" {
		t.Fatal("PaymentIntent.ClientSecret is empty")
	}

	updatedAmount := int64(3000)
	updatedStatus := "requires_capture"
	updated, err := sim.UpdatePaymentIntent(ctx, "payments", intent.ID, UpdatePaymentIntentParams{
		Amount: &updatedAmount,
		Status: &updatedStatus,
	})
	if err != nil {
		t.Fatalf("UpdatePaymentIntent() error = %v", err)
	}
	if updated.Amount != updatedAmount || updated.Status != updatedStatus {
		t.Fatalf("updated intent = %#v, want amount %d and status %q", updated, updatedAmount, updatedStatus)
	}

	readBack, err := sim.GetPaymentIntent(ctx, "payments", intent.ID)
	if err != nil {
		t.Fatalf("GetPaymentIntent() error = %v", err)
	}
	if readBack.Amount != updatedAmount || readBack.Status != updatedStatus {
		t.Fatalf("readBack intent = %#v, want updated state", readBack)
	}
}

func TestSimulatorRejectsInvalidPaymentIntentReferences(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "invalid-references"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := sim.CreatePaymentIntent(ctx, "invalid-references", CreatePaymentIntentParams{
		Amount:     1000,
		Currency:   "usd",
		CustomerID: "cus_missing",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreatePaymentIntent(missing customer) error = %v, want ErrNotFound", err)
	}

	if _, err := sim.CreatePaymentIntent(ctx, "invalid-references", CreatePaymentIntentParams{
		Amount:   0,
		Currency: "usd",
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CreatePaymentIntent(invalid amount) error = %v, want ErrInvalidRequest", err)
	}
}

func TestSimulatorRejectsPaymentMethodReattachToDifferentCustomer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "reattach"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	firstCustomer, err := sim.CreateCustomer(ctx, "reattach", CreateCustomerParams{Email: "first@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer(first) error = %v", err)
	}
	secondCustomer, err := sim.CreateCustomer(ctx, "reattach", CreateCustomerParams{Email: "second@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer(second) error = %v", err)
	}
	paymentMethod, err := sim.CreatePaymentMethod(ctx, "reattach", CreatePaymentMethodParams{
		Card: Card{Brand: "visa", Last4: "4242"},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}
	if _, err := sim.AttachPaymentMethod(ctx, "reattach", paymentMethod.ID, AttachPaymentMethodParams{
		CustomerID: firstCustomer.ID,
	}); err != nil {
		t.Fatalf("AttachPaymentMethod(first) error = %v", err)
	}

	if _, err := sim.AttachPaymentMethod(ctx, "reattach", paymentMethod.ID, AttachPaymentMethodParams{
		CustomerID: secondCustomer.ID,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("AttachPaymentMethod(second) error = %v, want ErrInvalidRequest", err)
	} else {
		var stripeErr *Error
		if !errors.As(err, &stripeErr) {
			t.Fatalf("AttachPaymentMethod(second) error = %T, want *Error", err)
		}
		if stripeErr.Code != "payment_method_unexpected_state" || stripeErr.Param != "customer" {
			t.Fatalf("stripe error code/param = %q/%q, want payment_method_unexpected_state/customer", stripeErr.Code, stripeErr.Param)
		}
	}
}

func TestSimulatorRejectsInvalidPaymentIntentStateTransitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "state-transitions"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	customer, err := sim.CreateCustomer(ctx, "state-transitions", CreateCustomerParams{Email: "payer@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	paymentMethod, err := sim.CreatePaymentMethod(ctx, "state-transitions", CreatePaymentMethodParams{
		Card: Card{Brand: "visa", Last4: "4242"},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}
	if _, err := sim.AttachPaymentMethod(ctx, "state-transitions", paymentMethod.ID, AttachPaymentMethodParams{
		CustomerID: customer.ID,
	}); err != nil {
		t.Fatalf("AttachPaymentMethod() error = %v", err)
	}
	intent, err := sim.CreatePaymentIntent(ctx, "state-transitions", CreatePaymentIntentParams{
		Amount:          5000,
		Currency:        "usd",
		CustomerID:      customer.ID,
		PaymentMethodID: paymentMethod.ID,
	})
	if err != nil {
		t.Fatalf("CreatePaymentIntent() error = %v", err)
	}

	invalidStatus := PaymentIntentStatusRequiresPaymentMethod
	if _, err := sim.UpdatePaymentIntent(ctx, "state-transitions", intent.ID, UpdatePaymentIntentParams{
		Status: &invalidStatus,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("UpdatePaymentIntent(invalid transition) error = %v, want ErrInvalidRequest", err)
	} else {
		var stripeErr *Error
		if !errors.As(err, &stripeErr) {
			t.Fatalf("UpdatePaymentIntent(invalid transition) error = %T, want *Error", err)
		}
		if stripeErr.Code != "payment_intent_unexpected_state" {
			t.Fatalf("stripe error code = %q, want payment_intent_unexpected_state", stripeErr.Code)
		}
	}

	succeededStatus := PaymentIntentStatusSucceeded
	succeeded, err := sim.UpdatePaymentIntent(ctx, "state-transitions", intent.ID, UpdatePaymentIntentParams{
		Status: &succeededStatus,
	})
	if err != nil {
		t.Fatalf("UpdatePaymentIntent(succeeded) error = %v", err)
	}
	updatedAmount := succeeded.Amount + 100
	if _, err := sim.UpdatePaymentIntent(ctx, "state-transitions", intent.ID, UpdatePaymentIntentParams{
		Amount: &updatedAmount,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("UpdatePaymentIntent(terminal amount) error = %v, want ErrInvalidRequest", err)
	}
}

func TestSimulatorInjectsDeterministicThirdCallFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	engine, err := injection.NewEngine([]injection.Rule{
		{
			Match: injection.Match{
				Service:   Service,
				Operation: OperationPaymentIntentsCreate,
				NthCall:   3,
			},
			Inject: injection.Inject{
				Library: "stripe.card_declined",
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	sim := newStripeTestSimulator(t, sqliteStore, WithErrorInjection(engine))
	if _, err := sim.CreateSession(ctx, "third-call-failure"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	customer, err := sim.CreateCustomer(ctx, "third-call-failure", CreateCustomerParams{Email: "payer@example.com"})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	paymentMethod, err := sim.CreatePaymentMethod(ctx, "third-call-failure", CreatePaymentMethodParams{
		Card: Card{Brand: "visa", Last4: "4242"},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}

	createIntent := func(amount int64) (PaymentIntent, error) {
		return sim.CreatePaymentIntent(ctx, "third-call-failure", CreatePaymentIntentParams{
			Amount:          amount,
			Currency:        "usd",
			CustomerID:      customer.ID,
			PaymentMethodID: paymentMethod.ID,
		})
	}

	if _, err := createIntent(1000); err != nil {
		t.Fatalf("first CreatePaymentIntent() error = %v", err)
	}
	if _, err := createIntent(2000); err != nil {
		t.Fatalf("second CreatePaymentIntent() error = %v", err)
	}

	if _, err := createIntent(3000); !errors.Is(err, ErrInjected) {
		t.Fatalf("third CreatePaymentIntent() error = %v, want ErrInjected", err)
	} else {
		var stripeErr *Error
		if !errors.As(err, &stripeErr) {
			t.Fatalf("third CreatePaymentIntent() error = %T, want *Error", err)
		}
		if stripeErr.StatusCode != 402 || stripeErr.Code != "card_declined" || stripeErr.Type != "card_error" {
			t.Fatalf("injected stripe error = %#v, want 402 card_error/card_declined", stripeErr)
		}
	}

	next, err := createIntent(4000)
	if err != nil {
		t.Fatalf("fourth CreatePaymentIntent() error = %v", err)
	}
	if next.ID != "pi_000003" {
		t.Fatalf("fourth PaymentIntent.ID = %q, want pi_000003 after failed third call did not mutate state", next.ID)
	}

	provenance := sim.AppliedErrorInjections()
	if len(provenance) != 1 {
		t.Fatalf("AppliedErrorInjections() len = %d, want 1", len(provenance))
	}
	if provenance[0].Operation != OperationPaymentIntentsCreate || provenance[0].CallNumber != 3 || provenance[0].Library != "stripe.card_declined" {
		t.Fatalf("provenance[0] = %#v, want third payment_intents.create card_declined", provenance[0])
	}

	metadata := sim.ErrorInjectionMetadata(map[string]any{"mode": "test"})
	section, ok := metadata[injection.MetadataKey].(map[string]any)
	if !ok {
		t.Fatalf("metadata[%q] = %#v, want map", injection.MetadataKey, metadata[injection.MetadataKey])
	}
	applied, ok := section["applied"].([]any)
	if !ok || len(applied) != 1 {
		t.Fatalf("metadata applied = %#v, want one provenance entry", section["applied"])
	}
}

func TestSimulatorInjectsExplicitResponseOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	engine, err := injection.NewEngine([]injection.Rule{
		{
			Match: injection.Match{
				Service:   Service,
				Operation: OperationCustomersCreate,
				AnyCall:   true,
			},
			Inject: injection.Inject{
				Status: 503,
				Body: map[string]any{
					"error": map[string]any{
						"type":    "api_connection_error",
						"code":    "maintenance",
						"message": "Injected maintenance window.",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	sim := newStripeTestSimulator(t, sqliteStore, WithErrorInjection(engine))
	if _, err := sim.CreateSession(ctx, "explicit-override"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := sim.CreateCustomer(ctx, "explicit-override", CreateCustomerParams{Email: "blocked@example.com"}); !errors.Is(err, ErrInjected) {
		t.Fatalf("CreateCustomer() error = %v, want ErrInjected", err)
	} else {
		var stripeErr *Error
		if !errors.As(err, &stripeErr) {
			t.Fatalf("CreateCustomer() error = %T, want *Error", err)
		}
		if stripeErr.StatusCode != 503 || stripeErr.Code != "maintenance" || stripeErr.Message != "Injected maintenance window." {
			t.Fatalf("injected stripe error = %#v, want explicit 503 maintenance body", stripeErr)
		}
	}

	if _, err := sim.GetCustomer(ctx, "explicit-override", "cus_000001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetCustomer() error = %v, want ErrNotFound because injected create did not mutate state", err)
	}
}

func TestSimulatorSchedulesStripeWebhooksForSupportedFlows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "webhooks"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	customer, err := sim.CreateCustomer(ctx, "webhooks", CreateCustomerParams{
		Email: "webhooks@example.com",
		Name:  "Webhook User",
	})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	updatedName := "Updated Webhook User"
	if _, err := sim.UpdateCustomer(ctx, "webhooks", customer.ID, UpdateCustomerParams{Name: &updatedName}); err != nil {
		t.Fatalf("UpdateCustomer() error = %v", err)
	}
	paymentMethod, err := sim.CreatePaymentMethod(ctx, "webhooks", CreatePaymentMethodParams{
		BillingDetails: BillingDetails{Phone: "+1-415-555-0100"},
		Card:           Card{Brand: "visa", Last4: "4242"},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}
	if _, err := sim.AttachPaymentMethod(ctx, "webhooks", paymentMethod.ID, AttachPaymentMethodParams{
		CustomerID: customer.ID,
	}); err != nil {
		t.Fatalf("AttachPaymentMethod() error = %v", err)
	}
	intent, err := sim.CreatePaymentIntent(ctx, "webhooks", CreatePaymentIntentParams{
		Amount:          1200,
		Currency:        "usd",
		CustomerID:      customer.ID,
		PaymentMethodID: paymentMethod.ID,
	})
	if err != nil {
		t.Fatalf("CreatePaymentIntent() error = %v", err)
	}
	succeededStatus := PaymentIntentStatusSucceeded
	if _, err := sim.UpdatePaymentIntent(ctx, "webhooks", intent.ID, UpdatePaymentIntentParams{
		Status: &succeededStatus,
	}); err != nil {
		t.Fatalf("UpdatePaymentIntent(succeeded) error = %v", err)
	}

	events, err := sqliteStore.ListDueScheduledEvents(
		ctx,
		"webhooks",
		store.ScheduledEventDeliveryModePush,
		time.Date(2026, time.April, 24, 13, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ListDueScheduledEvents() error = %v", err)
	}

	topics := map[string]bool{}
	for _, event := range events {
		topics[event.Topic] = true
		if event.Service != Service {
			t.Fatalf("event.Service = %q, want %q", event.Service, Service)
		}
		if event.Payload["object"] != "event" {
			t.Fatalf("event.Payload[object] = %#v, want event", event.Payload["object"])
		}
	}

	for _, topic := range []string{
		"webhook.customer.created",
		"webhook.customer.updated",
		"webhook.payment_method.attached",
		"webhook.payment_intent.created",
		"webhook.payment_intent.succeeded",
	} {
		if !topics[topic] {
			t.Fatalf("scheduled topics = %#v, missing %q", topics, topic)
		}
	}
}

func TestSimulatorExtractsCustomerIdentityFromCustomerAndPaymentMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openStripeTestStore(t)
	defer sqliteStore.Close()

	sim := newStripeTestSimulator(t, sqliteStore)
	if _, err := sim.CreateSession(ctx, "identity"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	customer, err := sim.CreateCustomer(ctx, "identity", CreateCustomerParams{
		Email: "identity@example.com",
		Metadata: map[string]string{
			"external_id": "acct_123",
		},
	})
	if err != nil {
		t.Fatalf("CreateCustomer() error = %v", err)
	}
	paymentMethod, err := sim.CreatePaymentMethod(ctx, "identity", CreatePaymentMethodParams{
		BillingDetails: BillingDetails{Name: "Identity User", Phone: "+1-415-555-0101"},
		Card:           Card{Brand: "visa", Last4: "4242"},
	})
	if err != nil {
		t.Fatalf("CreatePaymentMethod() error = %v", err)
	}
	if _, err := sim.AttachPaymentMethod(ctx, "identity", paymentMethod.ID, AttachPaymentMethodParams{
		CustomerID: customer.ID,
	}); err != nil {
		t.Fatalf("AttachPaymentMethod() error = %v", err)
	}

	identity, err := sim.ExtractCustomerIdentity(ctx, "identity", customer.ID)
	if err != nil {
		t.Fatalf("ExtractCustomerIdentity() error = %v", err)
	}
	if identity.CustomerID != customer.ID {
		t.Fatalf("CustomerID = %q, want %q", identity.CustomerID, customer.ID)
	}
	if identity.Email != "identity@example.com" {
		t.Fatalf("Email = %q, want identity@example.com", identity.Email)
	}
	if identity.Name != "Identity User" {
		t.Fatalf("Name = %q, want Identity User", identity.Name)
	}
	if identity.Phone != "+1-415-555-0101" {
		t.Fatalf("Phone = %q, want +1-415-555-0101", identity.Phone)
	}
	if len(identity.PaymentMethodIDs) != 1 || identity.PaymentMethodIDs[0] != paymentMethod.ID {
		t.Fatalf("PaymentMethodIDs = %#v, want [%q]", identity.PaymentMethodIDs, paymentMethod.ID)
	}
	if identity.Metadata["external_id"] != "acct_123" {
		t.Fatalf("Metadata[external_id] = %q, want acct_123", identity.Metadata["external_id"])
	}
}

func openStripeTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	return sqliteStore
}

func newStripeTestSimulator(t *testing.T, sessionStore store.SessionStore, opts ...Option) *Simulator {
	t.Helper()

	now := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	nextSnapshotID := 0
	nextEventID := 0
	baseOptions := []Option{
		WithClock(func() time.Time {
			now = now.Add(time.Second)
			return now
		}),
		WithSnapshotIDGenerator(func(prefix string) (string, error) {
			nextSnapshotID++
			return fmt.Sprintf("%s_test_%06d", prefix, nextSnapshotID), nil
		}),
		WithEventIDGenerator(func(prefix string) (string, error) {
			nextEventID++
			return fmt.Sprintf("%s_stripe_test_%06d", prefix, nextEventID), nil
		}),
	}
	baseOptions = append(baseOptions, opts...)

	sim, err := NewSimulator(sessionStore, baseOptions...)
	if err != nil {
		t.Fatalf("NewSimulator() error = %v", err)
	}
	return sim
}
