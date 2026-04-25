package stripe

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

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

func openStripeTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	return sqliteStore
}

func newStripeTestSimulator(t *testing.T, sessionStore store.SessionStore) *Simulator {
	t.Helper()

	now := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	nextSnapshotID := 0
	sim, err := NewSimulator(
		sessionStore,
		WithClock(func() time.Time {
			now = now.Add(time.Second)
			return now
		}),
		WithSnapshotIDGenerator(func(prefix string) (string, error) {
			nextSnapshotID++
			return fmt.Sprintf("%s_test_%06d", prefix, nextSnapshotID), nil
		}),
	)
	if err != nil {
		t.Fatalf("NewSimulator() error = %v", err)
	}
	return sim
}
