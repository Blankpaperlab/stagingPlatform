// Package stripe implements the first stateful Stripe simulator slice.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"stagehand/internal/runtime/injection"
	runtimequeue "stagehand/internal/runtime/queue"
	"stagehand/internal/runtime/session"
	"stagehand/internal/store"
)

const (
	stateKey = "stripe"

	Service = "stripe"

	OperationCustomersCreate        = "customers.create"
	OperationCustomersRetrieve      = "customers.retrieve"
	OperationCustomersSearch        = "customers.search"
	OperationCustomersUpdate        = "customers.update"
	OperationPaymentMethodsCreate   = "payment_methods.create"
	OperationPaymentMethodsRetrieve = "payment_methods.retrieve"
	OperationPaymentMethodsUpdate   = "payment_methods.update"
	OperationPaymentMethodsAttach   = "payment_methods.attach"
	OperationPaymentIntentsCreate   = "payment_intents.create"
	OperationPaymentIntentsRetrieve = "payment_intents.retrieve"
	OperationPaymentIntentsList     = "payment_intents.list"
	OperationPaymentIntentsUpdate   = "payment_intents.update"
	OperationRefundsCreate          = "refunds.create"

	PaymentIntentStatusRequiresPaymentMethod = "requires_payment_method"
	PaymentIntentStatusRequiresConfirmation  = "requires_confirmation"
	PaymentIntentStatusRequiresAction        = "requires_action"
	PaymentIntentStatusRequiresCapture       = "requires_capture"
	PaymentIntentStatusSucceeded             = "succeeded"
	PaymentIntentStatusCanceled              = "canceled"

	WebhookCustomerCreated               = "customer.created"
	WebhookCustomerUpdated               = "customer.updated"
	WebhookPaymentMethodAttached         = "payment_method.attached"
	WebhookPaymentIntentCreated          = "payment_intent.created"
	WebhookPaymentIntentSucceeded        = "payment_intent.succeeded"
	WebhookPaymentIntentCanceled         = "payment_intent.canceled"
	WebhookPaymentIntentAmountCapturable = "payment_intent.amount_capturable_updated"
	WebhookRefundCreated                 = "refund.created"
)

var (
	ErrInvalidRequest = errors.New("stripe invalid request")
	ErrNotFound       = errors.New("stripe object not found")
	ErrInjected       = errors.New("stripe injected error")
)

type Error struct {
	Type       string `json:"type"`
	Code       string `json:"code"`
	Param      string `json:"param,omitempty"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code"`
	cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Param != "" {
		return fmt.Sprintf("stripe %s: %s (param: %s, code: %s)", e.Type, e.Message, e.Param, e.Code)
	}
	return fmt.Sprintf("stripe %s: %s (code: %s)", e.Type, e.Message, e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

type Simulator struct {
	store           store.SessionStore
	sessions        *session.Manager
	eventQueue      *runtimequeue.Manager
	now             func() time.Time
	webhookDelay    time.Duration
	webhookDelivery store.ScheduledEventDeliveryMode
	errorInjector   *injection.Engine
	injectionMu     sync.Mutex
	injections      []injection.Provenance
	mu              sync.Mutex
}

type Option func(*simulatorOptions)

type simulatorOptions struct {
	now             func() time.Time
	snapshotNewID   func(prefix string) (string, error)
	eventStore      store.EventQueueStore
	eventNewID      func(prefix string) (string, error)
	webhookDelay    time.Duration
	webhookDelivery store.ScheduledEventDeliveryMode
	errorInjector   *injection.Engine
}

func WithClock(now func() time.Time) Option {
	return func(opts *simulatorOptions) {
		if now != nil {
			opts.now = now
		}
	}
}

func WithSnapshotIDGenerator(newID func(prefix string) (string, error)) Option {
	return func(opts *simulatorOptions) {
		if newID != nil {
			opts.snapshotNewID = newID
		}
	}
}

func WithEventQueueStore(eventStore store.EventQueueStore) Option {
	return func(opts *simulatorOptions) {
		if eventStore != nil {
			opts.eventStore = eventStore
		}
	}
}

func WithEventIDGenerator(newID func(prefix string) (string, error)) Option {
	return func(opts *simulatorOptions) {
		if newID != nil {
			opts.eventNewID = newID
		}
	}
}

func WithWebhookDelay(delay time.Duration) Option {
	return func(opts *simulatorOptions) {
		if delay >= 0 {
			opts.webhookDelay = delay
		}
	}
}

func WithErrorInjection(engine *injection.Engine) Option {
	return func(opts *simulatorOptions) {
		opts.errorInjector = engine
	}
}

func NewSimulator(sessionStore store.SessionStore, opts ...Option) (*Simulator, error) {
	if sessionStore == nil {
		return nil, fmt.Errorf("stripe simulator session store is required")
	}

	options := simulatorOptions{
		now: func() time.Time {
			return time.Now().UTC()
		},
		webhookDelay:    time.Minute,
		webhookDelivery: store.ScheduledEventDeliveryModePush,
	}
	for _, opt := range opts {
		opt(&options)
	}

	sessionOptions := []session.Option{
		session.WithClock(options.now),
	}
	if options.snapshotNewID != nil {
		sessionOptions = append(sessionOptions, session.WithIDGenerator(options.snapshotNewID))
	}

	sessionManager, err := session.NewManager(sessionStore, sessionOptions...)
	if err != nil {
		return nil, err
	}

	eventStore := options.eventStore
	if eventStore == nil {
		if inferredEventStore, ok := sessionStore.(store.EventQueueStore); ok {
			eventStore = inferredEventStore
		}
	}

	var eventQueue *runtimequeue.Manager
	if eventStore != nil {
		queueOptions := []runtimequeue.Option{
			runtimequeue.WithClock(options.now),
		}
		if options.eventNewID != nil {
			queueOptions = append(queueOptions, runtimequeue.WithIDGenerator(options.eventNewID))
		}
		eventQueue, err = runtimequeue.NewManager(eventStore, queueOptions...)
		if err != nil {
			return nil, err
		}
	}

	return &Simulator{
		store:           sessionStore,
		sessions:        sessionManager,
		eventQueue:      eventQueue,
		now:             options.now,
		webhookDelay:    options.webhookDelay,
		webhookDelivery: options.webhookDelivery,
		errorInjector:   options.errorInjector,
	}, nil
}

func (s *Simulator) CreateSession(ctx context.Context, sessionName string) (store.SessionRecord, error) {
	return s.sessions.Create(ctx, sessionName)
}

func (s *Simulator) AppliedErrorInjections() []injection.Provenance {
	s.injectionMu.Lock()
	defer s.injectionMu.Unlock()

	return append([]injection.Provenance(nil), s.injections...)
}

func (s *Simulator) ErrorInjectionMetadata(metadata map[string]any) map[string]any {
	for _, provenance := range s.AppliedErrorInjections() {
		metadata = injection.AppendProvenance(metadata, provenance)
	}
	return metadata
}

type State struct {
	Customers      map[string]Customer      `json:"customers"`
	PaymentMethods map[string]PaymentMethod `json:"payment_methods"`
	PaymentIntents map[string]PaymentIntent `json:"payment_intents"`
	Refunds        map[string]Refund        `json:"refunds"`
	Counters       Counters                 `json:"counters"`
}

type Counters struct {
	Customer      int `json:"customer"`
	PaymentMethod int `json:"payment_method"`
	PaymentIntent int `json:"payment_intent"`
	Refund        int `json:"refund"`
}

type Customer struct {
	ID          string            `json:"id"`
	Object      string            `json:"object"`
	Email       string            `json:"email,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Created     int64             `json:"created"`
}

type CustomerIdentity struct {
	CustomerID       string            `json:"customer_id"`
	Email            string            `json:"email,omitempty"`
	Name             string            `json:"name,omitempty"`
	Phone            string            `json:"phone,omitempty"`
	PaymentMethodIDs []string          `json:"payment_method_ids,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type BillingDetails struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type Card struct {
	Brand    string `json:"brand,omitempty"`
	Last4    string `json:"last4,omitempty"`
	ExpMonth int    `json:"exp_month,omitempty"`
	ExpYear  int    `json:"exp_year,omitempty"`
	Funding  string `json:"funding,omitempty"`
}

type PaymentMethod struct {
	ID             string            `json:"id"`
	Object         string            `json:"object"`
	Type           string            `json:"type"`
	CustomerID     string            `json:"customer,omitempty"`
	BillingDetails BillingDetails    `json:"billing_details,omitempty"`
	Card           Card              `json:"card,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Created        int64             `json:"created"`
}

type PaymentIntent struct {
	ID              string            `json:"id"`
	Object          string            `json:"object"`
	Amount          int64             `json:"amount"`
	AmountRefunded  int64             `json:"amount_refunded,omitempty"`
	Currency        string            `json:"currency"`
	CustomerID      string            `json:"customer,omitempty"`
	PaymentMethodID string            `json:"payment_method,omitempty"`
	Status          string            `json:"status"`
	Refunded        bool              `json:"refunded,omitempty"`
	ClientSecret    string            `json:"client_secret"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Created         int64             `json:"created"`
}

type Refund struct {
	ID              string            `json:"id"`
	Object          string            `json:"object"`
	Amount          int64             `json:"amount"`
	Currency        string            `json:"currency"`
	PaymentIntentID string            `json:"payment_intent"`
	Reason          string            `json:"reason,omitempty"`
	Status          string            `json:"status"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Created         int64             `json:"created"`
}

type CustomerSearchResult struct {
	Object  string     `json:"object"`
	Data    []Customer `json:"data"`
	HasMore bool       `json:"has_more"`
	URL     string     `json:"url"`
}

type PaymentIntentListResult struct {
	Object  string          `json:"object"`
	Data    []PaymentIntent `json:"data"`
	HasMore bool            `json:"has_more"`
	URL     string          `json:"url"`
}

type CreateCustomerParams struct {
	Email       string
	Name        string
	Description string
	Metadata    map[string]string
}

type UpdateCustomerParams struct {
	Email       *string
	Name        *string
	Description *string
	Metadata    map[string]string
}

type CreatePaymentMethodParams struct {
	Type           string
	BillingDetails BillingDetails
	Card           Card
	Metadata       map[string]string
}

type UpdatePaymentMethodParams struct {
	BillingDetails *BillingDetails
	Card           *Card
	Metadata       map[string]string
}

type AttachPaymentMethodParams struct {
	CustomerID string
}

type CreatePaymentIntentParams struct {
	Amount          int64
	Currency        string
	CustomerID      string
	PaymentMethodID string
	Status          string
	Metadata        map[string]string
}

type UpdatePaymentIntentParams struct {
	Amount          *int64
	Currency        *string
	CustomerID      *string
	PaymentMethodID *string
	Status          *string
	Metadata        map[string]string
}

type ListPaymentIntentsParams struct {
	CustomerID string
	Limit      int
}

type CreateRefundParams struct {
	PaymentIntentID string
	Amount          int64
	Reason          string
	Metadata        map[string]string
}

type Match struct {
	Service   string
	Operation string
	Method    string
	Path      string
	Params    map[string]string
}

func MatchRequest(method string, rawURL string) (Match, bool) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return Match{}, false
	}

	cleanPath := path.Clean("/" + strings.TrimPrefix(parsedURL.Path, "/"))
	if cleanPath == "/." {
		cleanPath = "/"
	}

	parts := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(parts) < 2 || parts[0] != "v1" {
		return Match{}, false
	}

	match := Match{
		Service: Service,
		Method:  normalizedMethod,
		Path:    cleanPath,
		Params:  map[string]string{},
	}

	switch {
	case normalizedMethod == "POST" && len(parts) == 2 && parts[1] == "customers":
		match.Operation = OperationCustomersCreate
		return match, true
	case normalizedMethod == "GET" && len(parts) == 3 && parts[1] == "customers" && parts[2] == "search":
		match.Operation = OperationCustomersSearch
		return match, true
	case normalizedMethod == "GET" && len(parts) == 3 && parts[1] == "customers":
		match.Operation = OperationCustomersRetrieve
		match.Params["customer_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 3 && parts[1] == "customers":
		match.Operation = OperationCustomersUpdate
		match.Params["customer_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 2 && parts[1] == "payment_methods":
		match.Operation = OperationPaymentMethodsCreate
		return match, true
	case normalizedMethod == "GET" && len(parts) == 3 && parts[1] == "payment_methods":
		match.Operation = OperationPaymentMethodsRetrieve
		match.Params["payment_method_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 3 && parts[1] == "payment_methods":
		match.Operation = OperationPaymentMethodsUpdate
		match.Params["payment_method_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 4 && parts[1] == "payment_methods" && parts[3] == "attach":
		match.Operation = OperationPaymentMethodsAttach
		match.Params["payment_method_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 2 && parts[1] == "payment_intents":
		match.Operation = OperationPaymentIntentsCreate
		return match, true
	case normalizedMethod == "GET" && len(parts) == 2 && parts[1] == "payment_intents":
		match.Operation = OperationPaymentIntentsList
		return match, true
	case normalizedMethod == "GET" && len(parts) == 3 && parts[1] == "payment_intents":
		match.Operation = OperationPaymentIntentsRetrieve
		match.Params["payment_intent_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 3 && parts[1] == "payment_intents":
		match.Operation = OperationPaymentIntentsUpdate
		match.Params["payment_intent_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 2 && parts[1] == "refunds":
		match.Operation = OperationRefundsCreate
		return match, true
	default:
		return Match{}, false
	}
}

func (s *Simulator) CreateCustomer(ctx context.Context, sessionName string, params CreateCustomerParams) (Customer, error) {
	if err := s.maybeInject(OperationCustomersCreate); err != nil {
		return Customer{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return Customer{}, err
	}

	state.Counters.Customer++
	customer := Customer{
		ID:          fmt.Sprintf("cus_%06d", state.Counters.Customer),
		Object:      "customer",
		Email:       strings.TrimSpace(params.Email),
		Name:        strings.TrimSpace(params.Name),
		Description: strings.TrimSpace(params.Description),
		Metadata:    cloneStringMap(params.Metadata),
		Created:     s.now().Unix(),
	}
	state.Customers[customer.ID] = customer

	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return Customer{}, err
	}
	if err := s.scheduleWebhook(ctx, sessionName, WebhookCustomerCreated, operationTime(customer.Created), customer, CustomerIdentity{
		CustomerID: customer.ID,
		Email:      customer.Email,
		Name:       customer.Name,
		Metadata:   cloneStringMap(customer.Metadata),
	}); err != nil {
		return Customer{}, err
	}
	return customer, nil
}

func (s *Simulator) GetCustomer(ctx context.Context, sessionName string, customerID string) (Customer, error) {
	if err := s.maybeInject(OperationCustomersRetrieve); err != nil {
		return Customer{}, err
	}

	state, _, err := s.loadState(ctx, sessionName)
	if err != nil {
		return Customer{}, err
	}

	customer, ok := state.Customers[strings.TrimSpace(customerID)]
	if !ok {
		return Customer{}, objectNotFound("customer", customerID)
	}
	return customer, nil
}

func (s *Simulator) SearchCustomers(ctx context.Context, sessionName string, query string) (CustomerSearchResult, error) {
	if err := s.maybeInject(OperationCustomersSearch); err != nil {
		return CustomerSearchResult{}, err
	}

	state, _, err := s.loadState(ctx, sessionName)
	if err != nil {
		return CustomerSearchResult{}, err
	}

	email, err := customerSearchEmail(query)
	if err != nil {
		return CustomerSearchResult{}, err
	}

	customers := make([]Customer, 0)
	for _, customer := range state.Customers {
		if strings.EqualFold(customer.Email, email) {
			customers = append(customers, customer)
		}
	}
	sort.Slice(customers, func(i, j int) bool {
		return customers[i].ID < customers[j].ID
	})

	return CustomerSearchResult{
		Object:  "search_result",
		Data:    customers,
		HasMore: false,
		URL:     "/v1/customers/search",
	}, nil
}

func (s *Simulator) ExtractCustomerIdentity(ctx context.Context, sessionName string, customerID string) (CustomerIdentity, error) {
	state, _, err := s.loadState(ctx, sessionName)
	if err != nil {
		return CustomerIdentity{}, err
	}
	return state.extractCustomerIdentity(customerID)
}

func (s *Simulator) UpdateCustomer(ctx context.Context, sessionName string, customerID string, params UpdateCustomerParams) (Customer, error) {
	if err := s.maybeInject(OperationCustomersUpdate); err != nil {
		return Customer{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return Customer{}, err
	}

	customerID = strings.TrimSpace(customerID)
	customer, ok := state.Customers[customerID]
	if !ok {
		return Customer{}, objectNotFound("customer", customerID)
	}

	if params.Email != nil {
		customer.Email = strings.TrimSpace(*params.Email)
	}
	if params.Name != nil {
		customer.Name = strings.TrimSpace(*params.Name)
	}
	if params.Description != nil {
		customer.Description = strings.TrimSpace(*params.Description)
	}
	if params.Metadata != nil {
		customer.Metadata = cloneStringMap(params.Metadata)
	}

	state.Customers[customer.ID] = customer
	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return Customer{}, err
	}
	identity, err := state.extractCustomerIdentity(customer.ID)
	if err != nil {
		return Customer{}, err
	}
	if err := s.scheduleWebhook(ctx, sessionName, WebhookCustomerUpdated, s.now(), customer, identity); err != nil {
		return Customer{}, err
	}
	return customer, nil
}

func (s *Simulator) CreatePaymentMethod(ctx context.Context, sessionName string, params CreatePaymentMethodParams) (PaymentMethod, error) {
	if err := s.maybeInject(OperationPaymentMethodsCreate); err != nil {
		return PaymentMethod{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentMethod{}, err
	}

	paymentMethodType := strings.TrimSpace(params.Type)
	if paymentMethodType == "" {
		paymentMethodType = "card"
	}
	if err := validatePaymentMethod(paymentMethodType, params.Card); err != nil {
		return PaymentMethod{}, err
	}

	state.Counters.PaymentMethod++
	paymentMethod := PaymentMethod{
		ID:             fmt.Sprintf("pm_%06d", state.Counters.PaymentMethod),
		Object:         "payment_method",
		Type:           paymentMethodType,
		BillingDetails: params.BillingDetails,
		Card:           params.Card,
		Metadata:       cloneStringMap(params.Metadata),
		Created:        s.now().Unix(),
	}
	state.PaymentMethods[paymentMethod.ID] = paymentMethod

	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentMethod{}, err
	}
	return paymentMethod, nil
}

func (s *Simulator) GetPaymentMethod(ctx context.Context, sessionName string, paymentMethodID string) (PaymentMethod, error) {
	if err := s.maybeInject(OperationPaymentMethodsRetrieve); err != nil {
		return PaymentMethod{}, err
	}

	state, _, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentMethod{}, err
	}

	paymentMethod, ok := state.PaymentMethods[strings.TrimSpace(paymentMethodID)]
	if !ok {
		return PaymentMethod{}, objectNotFound("payment_method", paymentMethodID)
	}
	return paymentMethod, nil
}

func (s *Simulator) UpdatePaymentMethod(ctx context.Context, sessionName string, paymentMethodID string, params UpdatePaymentMethodParams) (PaymentMethod, error) {
	if err := s.maybeInject(OperationPaymentMethodsUpdate); err != nil {
		return PaymentMethod{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentMethod{}, err
	}

	paymentMethodID = strings.TrimSpace(paymentMethodID)
	paymentMethod, ok := state.PaymentMethods[paymentMethodID]
	if !ok {
		return PaymentMethod{}, objectNotFound("payment_method", paymentMethodID)
	}

	if params.BillingDetails != nil {
		paymentMethod.BillingDetails = *params.BillingDetails
	}
	if params.Card != nil {
		if err := validatePaymentMethod(paymentMethod.Type, *params.Card); err != nil {
			return PaymentMethod{}, err
		}
		paymentMethod.Card = *params.Card
	}
	if params.Metadata != nil {
		paymentMethod.Metadata = cloneStringMap(params.Metadata)
	}

	state.PaymentMethods[paymentMethod.ID] = paymentMethod
	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentMethod{}, err
	}
	return paymentMethod, nil
}

func (s *Simulator) AttachPaymentMethod(ctx context.Context, sessionName string, paymentMethodID string, params AttachPaymentMethodParams) (PaymentMethod, error) {
	if err := s.maybeInject(OperationPaymentMethodsAttach); err != nil {
		return PaymentMethod{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentMethod{}, err
	}

	customerID := strings.TrimSpace(params.CustomerID)
	if _, ok := state.Customers[customerID]; !ok {
		return PaymentMethod{}, objectNotFound("customer", customerID)
	}

	paymentMethodID = strings.TrimSpace(paymentMethodID)
	paymentMethod, ok := state.PaymentMethods[paymentMethodID]
	if !ok {
		return PaymentMethod{}, objectNotFound("payment_method", paymentMethodID)
	}
	if paymentMethod.CustomerID != "" && paymentMethod.CustomerID != customerID {
		return PaymentMethod{}, stripeInvalid(
			"payment_method_unexpected_state",
			"customer",
			fmt.Sprintf("Payment method %s is already attached to customer %s.", paymentMethod.ID, paymentMethod.CustomerID),
		)
	}
	alreadyAttached := paymentMethod.CustomerID == customerID
	paymentMethod.CustomerID = customerID

	state.PaymentMethods[paymentMethod.ID] = paymentMethod
	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentMethod{}, err
	}
	if !alreadyAttached {
		identity, err := state.extractCustomerIdentity(customerID)
		if err != nil {
			return PaymentMethod{}, err
		}
		if err := s.scheduleWebhook(ctx, sessionName, WebhookPaymentMethodAttached, s.now(), paymentMethod, identity); err != nil {
			return PaymentMethod{}, err
		}
	}
	return paymentMethod, nil
}

func (s *Simulator) CreatePaymentIntent(ctx context.Context, sessionName string, params CreatePaymentIntentParams) (PaymentIntent, error) {
	if err := s.maybeInject(OperationPaymentIntentsCreate); err != nil {
		return PaymentIntent{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentIntent{}, err
	}
	if err := validatePaymentIntentReferences(state, params.CustomerID, params.PaymentMethodID); err != nil {
		return PaymentIntent{}, err
	}
	if params.Amount <= 0 {
		return PaymentIntent{}, stripeInvalid("parameter_invalid_integer", "amount", "PaymentIntent amount must be greater than 0.")
	}

	currency := normalizeCurrency(params.Currency)
	if currency == "" {
		return PaymentIntent{}, stripeInvalid("parameter_missing", "currency", "PaymentIntent currency is required.")
	}

	customerID, paymentMethodID, status, err := normalizePaymentIntentCreate(state, params.CustomerID, params.PaymentMethodID, params.Status)
	if err != nil {
		return PaymentIntent{}, err
	}

	state.Counters.PaymentIntent++
	paymentIntent := PaymentIntent{
		ID:              fmt.Sprintf("pi_%06d", state.Counters.PaymentIntent),
		Object:          "payment_intent",
		Amount:          params.Amount,
		Currency:        currency,
		CustomerID:      customerID,
		PaymentMethodID: paymentMethodID,
		Status:          status,
		Metadata:        cloneStringMap(params.Metadata),
		Created:         s.now().Unix(),
	}
	paymentIntent.ClientSecret = paymentIntent.ID + "_secret_stagehand"
	state.PaymentIntents[paymentIntent.ID] = paymentIntent

	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentIntent{}, err
	}
	if err := s.schedulePaymentIntentWebhooks(ctx, sessionName, paymentIntent, "", paymentIntent.Status, state); err != nil {
		return PaymentIntent{}, err
	}
	return paymentIntent, nil
}

func (s *Simulator) GetPaymentIntent(ctx context.Context, sessionName string, paymentIntentID string) (PaymentIntent, error) {
	if err := s.maybeInject(OperationPaymentIntentsRetrieve); err != nil {
		return PaymentIntent{}, err
	}

	state, _, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentIntent{}, err
	}

	paymentIntent, ok := state.PaymentIntents[strings.TrimSpace(paymentIntentID)]
	if !ok {
		return PaymentIntent{}, objectNotFound("payment_intent", paymentIntentID)
	}
	return paymentIntent, nil
}

func (s *Simulator) ListPaymentIntents(ctx context.Context, sessionName string, params ListPaymentIntentsParams) (PaymentIntentListResult, error) {
	if err := s.maybeInject(OperationPaymentIntentsList); err != nil {
		return PaymentIntentListResult{}, err
	}

	state, _, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentIntentListResult{}, err
	}

	customerID := strings.TrimSpace(params.CustomerID)
	if customerID != "" {
		if _, ok := state.Customers[customerID]; !ok {
			return PaymentIntentListResult{}, objectNotFound("customer", customerID)
		}
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}

	intents := make([]PaymentIntent, 0)
	for _, paymentIntent := range state.PaymentIntents {
		if customerID != "" && paymentIntent.CustomerID != customerID {
			continue
		}
		intents = append(intents, paymentIntent)
	}
	sort.Slice(intents, func(i, j int) bool {
		if intents[i].Created == intents[j].Created {
			return intents[i].ID > intents[j].ID
		}
		return intents[i].Created > intents[j].Created
	})

	hasMore := len(intents) > limit
	if hasMore {
		intents = intents[:limit]
	}

	return PaymentIntentListResult{
		Object:  "list",
		Data:    intents,
		HasMore: hasMore,
		URL:     "/v1/payment_intents",
	}, nil
}

func (s *Simulator) UpdatePaymentIntent(ctx context.Context, sessionName string, paymentIntentID string, params UpdatePaymentIntentParams) (PaymentIntent, error) {
	if err := s.maybeInject(OperationPaymentIntentsUpdate); err != nil {
		return PaymentIntent{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return PaymentIntent{}, err
	}

	paymentIntentID = strings.TrimSpace(paymentIntentID)
	paymentIntent, ok := state.PaymentIntents[paymentIntentID]
	if !ok {
		return PaymentIntent{}, objectNotFound("payment_intent", paymentIntentID)
	}

	previousStatus := paymentIntent.Status
	if paymentIntentTerminal(previousStatus) && updateMutatesTerminalPaymentIntent(params, paymentIntent) {
		return PaymentIntent{}, stripeInvalid(
			"payment_intent_unexpected_state",
			"",
			fmt.Sprintf("Cannot update PaymentIntent %s because it is already %s.", paymentIntent.ID, previousStatus),
		)
	}

	nextAmount := paymentIntent.Amount
	if params.Amount != nil {
		if *params.Amount <= 0 {
			return PaymentIntent{}, stripeInvalid("parameter_invalid_integer", "amount", "PaymentIntent amount must be greater than 0.")
		}
		nextAmount = *params.Amount
	}

	nextCurrency := paymentIntent.Currency
	if params.Currency != nil {
		currency := normalizeCurrency(*params.Currency)
		if currency == "" {
			return PaymentIntent{}, stripeInvalid("parameter_missing", "currency", "PaymentIntent currency is required.")
		}
		nextCurrency = currency
	}

	nextCustomerID := paymentIntent.CustomerID
	if params.CustomerID != nil {
		nextCustomerID = strings.TrimSpace(*params.CustomerID)
	}

	nextPaymentMethodID := paymentIntent.PaymentMethodID
	if params.PaymentMethodID != nil {
		nextPaymentMethodID = strings.TrimSpace(*params.PaymentMethodID)
	}

	nextStatus := paymentIntent.Status
	if params.Status != nil {
		nextStatus = strings.TrimSpace(*params.Status)
	}
	nextCustomerID, nextPaymentMethodID, nextStatus, err = normalizePaymentIntentUpdate(
		state,
		paymentIntent,
		nextCustomerID,
		nextPaymentMethodID,
		nextStatus,
		params.Status != nil,
	)
	if err != nil {
		return PaymentIntent{}, err
	}

	paymentIntent.Amount = nextAmount
	paymentIntent.Currency = nextCurrency
	paymentIntent.CustomerID = nextCustomerID
	paymentIntent.PaymentMethodID = nextPaymentMethodID
	paymentIntent.Status = nextStatus
	if params.Metadata != nil {
		paymentIntent.Metadata = cloneStringMap(params.Metadata)
	}

	state.PaymentIntents[paymentIntent.ID] = paymentIntent
	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentIntent{}, err
	}
	if err := s.schedulePaymentIntentWebhooks(ctx, sessionName, paymentIntent, previousStatus, paymentIntent.Status, state); err != nil {
		return PaymentIntent{}, err
	}
	return paymentIntent, nil
}

func (s *Simulator) CreateRefund(ctx context.Context, sessionName string, params CreateRefundParams) (Refund, error) {
	if err := s.maybeInject(OperationRefundsCreate); err != nil {
		return Refund{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, sessionState, err := s.loadState(ctx, sessionName)
	if err != nil {
		return Refund{}, err
	}

	paymentIntentID := strings.TrimSpace(params.PaymentIntentID)
	if paymentIntentID == "" {
		return Refund{}, stripeInvalid("parameter_missing", "payment_intent", "Refund payment_intent is required.")
	}
	paymentIntent, ok := state.PaymentIntents[paymentIntentID]
	if !ok {
		return Refund{}, objectNotFound("payment_intent", paymentIntentID)
	}
	if paymentIntent.Status != PaymentIntentStatusSucceeded {
		return Refund{}, stripeInvalid(
			"payment_intent_unexpected_state",
			"payment_intent",
			fmt.Sprintf("Cannot refund PaymentIntent %s because it is %s.", paymentIntent.ID, paymentIntent.Status),
		)
	}

	remaining := paymentIntent.Amount - paymentIntent.AmountRefunded
	amount := params.Amount
	if amount == 0 {
		amount = remaining
	}
	if amount <= 0 {
		return Refund{}, stripeInvalid("parameter_invalid_integer", "amount", "Refund amount must be greater than 0.")
	}
	if amount > remaining {
		return Refund{}, stripeInvalid(
			"amount_too_large",
			"amount",
			fmt.Sprintf("Refund amount %d exceeds remaining refundable amount %d.", amount, remaining),
		)
	}

	state.Counters.Refund++
	refund := Refund{
		ID:              fmt.Sprintf("re_%06d", state.Counters.Refund),
		Object:          "refund",
		Amount:          amount,
		Currency:        paymentIntent.Currency,
		PaymentIntentID: paymentIntent.ID,
		Reason:          strings.TrimSpace(params.Reason),
		Status:          "succeeded",
		Metadata:        cloneStringMap(params.Metadata),
		Created:         s.now().Unix(),
	}
	state.Refunds[refund.ID] = refund

	paymentIntent.AmountRefunded += amount
	paymentIntent.Refunded = paymentIntent.AmountRefunded >= paymentIntent.Amount
	state.PaymentIntents[paymentIntent.ID] = paymentIntent

	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return Refund{}, err
	}

	identity := CustomerIdentity{}
	if paymentIntent.CustomerID != "" {
		identity, err = state.extractCustomerIdentity(paymentIntent.CustomerID)
		if err != nil {
			return Refund{}, err
		}
	}
	if err := s.scheduleWebhook(ctx, sessionName, WebhookRefundCreated, operationTime(refund.Created), refund, identity); err != nil {
		return Refund{}, err
	}
	return refund, nil
}

func (s *Simulator) loadState(ctx context.Context, sessionName string) (State, map[string]any, error) {
	sessionName = strings.TrimSpace(sessionName)
	if sessionName == "" {
		return State{}, nil, invalidRequest("session name is required")
	}

	if _, err := s.store.GetSession(ctx, sessionName); err != nil {
		return State{}, nil, err
	}

	snapshot, err := s.sessions.CurrentSnapshot(ctx, sessionName)
	if errors.Is(err, store.ErrNotFound) {
		return newState(), map[string]any{}, nil
	}
	if err != nil {
		return State{}, nil, err
	}

	sessionState := cloneStateMap(snapshot.State)
	rawStripeState, ok := sessionState[stateKey]
	if !ok || rawStripeState == nil {
		return newState(), sessionState, nil
	}

	encoded, err := json.Marshal(rawStripeState)
	if err != nil {
		return State{}, nil, fmt.Errorf("marshal stripe session state: %w", err)
	}

	var state State
	if err := json.Unmarshal(encoded, &state); err != nil {
		return State{}, nil, fmt.Errorf("unmarshal stripe session state: %w", err)
	}
	state.normalize()
	return state, sessionState, nil
}

func (s *Simulator) saveState(ctx context.Context, sessionName string, sessionState map[string]any, state State) error {
	state.normalize()
	sessionState[stateKey] = state
	_, err := s.sessions.Snapshot(ctx, sessionName, sessionState)
	if err != nil {
		return fmt.Errorf("snapshot stripe session state: %w", err)
	}
	return nil
}

func (s *Simulator) maybeInject(operation string) error {
	if s.errorInjector == nil {
		return nil
	}

	decision, err := s.errorInjector.Evaluate(injection.Request{
		Service:   Service,
		Operation: operation,
	})
	if err != nil {
		return err
	}
	if !decision.Matched {
		return nil
	}

	s.injectionMu.Lock()
	s.injections = append(s.injections, decision.Provenance)
	s.injectionMu.Unlock()
	return injectedStripeError(decision.Override)
}

func (s *Simulator) schedulePaymentIntentWebhooks(
	ctx context.Context,
	sessionName string,
	paymentIntent PaymentIntent,
	previousStatus string,
	currentStatus string,
	state State,
) error {
	identity := CustomerIdentity{}
	if paymentIntent.CustomerID != "" {
		extracted, err := state.extractCustomerIdentity(paymentIntent.CustomerID)
		if err != nil {
			return err
		}
		identity = extracted
	}

	occurredAt := operationTime(paymentIntent.Created)
	if previousStatus == "" {
		if err := s.scheduleWebhook(ctx, sessionName, WebhookPaymentIntentCreated, occurredAt, paymentIntent, identity); err != nil {
			return err
		}
	}

	if previousStatus == currentStatus {
		return nil
	}

	eventType := paymentIntentStatusWebhook(currentStatus)
	if eventType == "" {
		return nil
	}
	return s.scheduleWebhook(ctx, sessionName, eventType, s.now(), paymentIntent, identity)
}

func (s *Simulator) scheduleWebhook(
	ctx context.Context,
	sessionName string,
	eventType string,
	occurredAt time.Time,
	object any,
	identity CustomerIdentity,
) error {
	if s.eventQueue == nil {
		return nil
	}
	if occurredAt.IsZero() {
		occurredAt = s.now()
	}

	payload := map[string]any{
		"object": "event",
		"type":   eventType,
		"data": map[string]any{
			"object": object,
		},
	}
	if identity.CustomerID != "" {
		payload["customer_identity"] = identity
	}

	_, err := s.eventQueue.Schedule(ctx, runtimequeue.ScheduleOptions{
		SessionName:  sessionName,
		Service:      Service,
		Topic:        "webhook." + eventType,
		DeliveryMode: s.webhookDelivery,
		DueAt:        occurredAt.Add(s.webhookDelay),
		Payload:      payload,
	})
	if err != nil {
		return fmt.Errorf("schedule stripe webhook %q: %w", eventType, err)
	}
	return nil
}

func (state State) extractCustomerIdentity(customerID string) (CustomerIdentity, error) {
	customerID = strings.TrimSpace(customerID)
	customer, ok := state.Customers[customerID]
	if !ok {
		return CustomerIdentity{}, objectNotFound("customer", customerID)
	}

	identity := CustomerIdentity{
		CustomerID: customer.ID,
		Email:      customer.Email,
		Name:       customer.Name,
		Metadata:   cloneStringMap(customer.Metadata),
	}

	for _, paymentMethod := range state.PaymentMethods {
		if paymentMethod.CustomerID != customer.ID {
			continue
		}
		identity.PaymentMethodIDs = append(identity.PaymentMethodIDs, paymentMethod.ID)
		if identity.Email == "" {
			identity.Email = paymentMethod.BillingDetails.Email
		}
		if identity.Name == "" {
			identity.Name = paymentMethod.BillingDetails.Name
		}
		if identity.Phone == "" {
			identity.Phone = paymentMethod.BillingDetails.Phone
		}
	}

	sort.Strings(identity.PaymentMethodIDs)
	return identity, nil
}

func newState() State {
	state := State{}
	state.normalize()
	return state
}

func (s *State) normalize() {
	if s.Customers == nil {
		s.Customers = map[string]Customer{}
	}
	if s.PaymentMethods == nil {
		s.PaymentMethods = map[string]PaymentMethod{}
	}
	if s.PaymentIntents == nil {
		s.PaymentIntents = map[string]PaymentIntent{}
	}
	if s.Refunds == nil {
		s.Refunds = map[string]Refund{}
	}
}

func validatePaymentIntentReferences(state State, customerID string, paymentMethodID string) error {
	customerID = strings.TrimSpace(customerID)
	if customerID != "" {
		if _, ok := state.Customers[customerID]; !ok {
			return objectNotFound("customer", customerID)
		}
	}

	paymentMethodID = strings.TrimSpace(paymentMethodID)
	if paymentMethodID != "" {
		if _, ok := state.PaymentMethods[paymentMethodID]; !ok {
			return objectNotFound("payment_method", paymentMethodID)
		}
	}

	return nil
}

func normalizePaymentIntentCreate(
	state State,
	customerID string,
	paymentMethodID string,
	status string,
) (string, string, string, error) {
	customerID, paymentMethodID, err := normalizePaymentIntentReferences(state, customerID, paymentMethodID)
	if err != nil {
		return "", "", "", err
	}

	status = normalizePaymentIntentStatus(status, paymentMethodID)
	if err := validatePaymentIntentStatus(status); err != nil {
		return "", "", "", err
	}
	if err := validatePaymentIntentStatusConsistency(status, paymentMethodID); err != nil {
		return "", "", "", err
	}

	return customerID, paymentMethodID, status, nil
}

func normalizePaymentIntentUpdate(
	state State,
	current PaymentIntent,
	customerID string,
	paymentMethodID string,
	status string,
	statusWasRequested bool,
) (string, string, string, error) {
	customerID, paymentMethodID, err := normalizePaymentIntentReferences(state, customerID, paymentMethodID)
	if err != nil {
		return "", "", "", err
	}

	if strings.TrimSpace(status) == "" {
		status = current.Status
	}
	status = strings.TrimSpace(status)
	if !statusWasRequested && current.Status == PaymentIntentStatusRequiresPaymentMethod && strings.TrimSpace(paymentMethodID) != "" {
		status = PaymentIntentStatusRequiresConfirmation
	}
	if err := validatePaymentIntentStatus(status); err != nil {
		return "", "", "", err
	}
	if err := validatePaymentIntentStatusConsistency(status, paymentMethodID); err != nil {
		return "", "", "", err
	}
	if statusWasRequested {
		if err := validatePaymentIntentTransition(current.Status, status); err != nil {
			return "", "", "", err
		}
	}

	return customerID, paymentMethodID, status, nil
}

func normalizePaymentIntentReferences(state State, customerID string, paymentMethodID string) (string, string, error) {
	customerID = strings.TrimSpace(customerID)
	paymentMethodID = strings.TrimSpace(paymentMethodID)

	if customerID != "" {
		if _, ok := state.Customers[customerID]; !ok {
			return "", "", objectNotFound("customer", customerID)
		}
	}

	if paymentMethodID == "" {
		return customerID, paymentMethodID, nil
	}

	paymentMethod, ok := state.PaymentMethods[paymentMethodID]
	if !ok {
		return "", "", objectNotFound("payment_method", paymentMethodID)
	}
	if paymentMethod.CustomerID != "" {
		if customerID != "" && customerID != paymentMethod.CustomerID {
			return "", "", stripeInvalid(
				"payment_method_customer_mismatch",
				"payment_method",
				fmt.Sprintf("Payment method %s is attached to customer %s and cannot be used with customer %s.", paymentMethodID, paymentMethod.CustomerID, customerID),
			)
		}
		customerID = paymentMethod.CustomerID
	}

	return customerID, paymentMethodID, nil
}

func customerSearchEmail(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", stripeInvalid("parameter_missing", "query", "Customer search query is required.")
	}

	const prefix = "email:"
	if !strings.HasPrefix(strings.ToLower(query), prefix) {
		return "", stripeInvalid("parameter_invalid_string", "query", "Only email customer search queries are supported.")
	}

	value := strings.TrimSpace(query[len(prefix):])
	value = strings.Trim(value, `"'`)
	if value == "" {
		return "", stripeInvalid("parameter_invalid_string", "query", "Customer search email cannot be empty.")
	}
	return value, nil
}

func normalizeCurrency(currency string) string {
	return strings.ToLower(strings.TrimSpace(currency))
}

func normalizePaymentIntentStatus(status string, paymentMethodID string) string {
	status = strings.TrimSpace(status)
	if status != "" {
		return status
	}
	if strings.TrimSpace(paymentMethodID) != "" {
		return PaymentIntentStatusRequiresConfirmation
	}
	return PaymentIntentStatusRequiresPaymentMethod
}

func validatePaymentIntentStatus(status string) error {
	switch status {
	case PaymentIntentStatusRequiresPaymentMethod,
		PaymentIntentStatusRequiresConfirmation,
		PaymentIntentStatusRequiresAction,
		PaymentIntentStatusRequiresCapture,
		PaymentIntentStatusSucceeded,
		PaymentIntentStatusCanceled:
		return nil
	default:
		return stripeInvalid("parameter_invalid_enum", "status", fmt.Sprintf("PaymentIntent status %q is not supported by the Stagehand Stripe simulator.", status))
	}
}

func validatePaymentIntentStatusConsistency(status string, paymentMethodID string) error {
	hasPaymentMethod := strings.TrimSpace(paymentMethodID) != ""
	if status == PaymentIntentStatusRequiresPaymentMethod && hasPaymentMethod {
		return stripeInvalid("payment_intent_unexpected_state", "status", "A PaymentIntent with a payment_method cannot be set to requires_payment_method.")
	}
	if status != PaymentIntentStatusRequiresPaymentMethod && !hasPaymentMethod {
		return stripeInvalid("parameter_missing", "payment_method", fmt.Sprintf("A payment_method is required before a PaymentIntent can be %s.", status))
	}
	return nil
}

func validatePaymentIntentTransition(from string, to string) error {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from == to {
		return nil
	}

	allowed := map[string][]string{
		PaymentIntentStatusRequiresPaymentMethod: {
			PaymentIntentStatusRequiresConfirmation,
			PaymentIntentStatusCanceled,
		},
		PaymentIntentStatusRequiresConfirmation: {
			PaymentIntentStatusRequiresAction,
			PaymentIntentStatusRequiresCapture,
			PaymentIntentStatusSucceeded,
			PaymentIntentStatusCanceled,
		},
		PaymentIntentStatusRequiresAction: {
			PaymentIntentStatusRequiresConfirmation,
			PaymentIntentStatusCanceled,
		},
		PaymentIntentStatusRequiresCapture: {
			PaymentIntentStatusSucceeded,
			PaymentIntentStatusCanceled,
		},
	}

	for _, candidate := range allowed[from] {
		if candidate == to {
			return nil
		}
	}

	return stripeInvalid(
		"payment_intent_unexpected_state",
		"status",
		fmt.Sprintf("Cannot transition PaymentIntent from %s to %s.", from, to),
	)
}

func paymentIntentTerminal(status string) bool {
	return status == PaymentIntentStatusSucceeded || status == PaymentIntentStatusCanceled
}

func updateMutatesTerminalPaymentIntent(params UpdatePaymentIntentParams, current PaymentIntent) bool {
	if params.Amount != nil && *params.Amount != current.Amount {
		return true
	}
	if params.Currency != nil && normalizeCurrency(*params.Currency) != current.Currency {
		return true
	}
	if params.CustomerID != nil && strings.TrimSpace(*params.CustomerID) != current.CustomerID {
		return true
	}
	if params.PaymentMethodID != nil && strings.TrimSpace(*params.PaymentMethodID) != current.PaymentMethodID {
		return true
	}
	if params.Status != nil && strings.TrimSpace(*params.Status) != current.Status {
		return true
	}
	return false
}

func paymentIntentStatusWebhook(status string) string {
	switch status {
	case PaymentIntentStatusRequiresCapture:
		return WebhookPaymentIntentAmountCapturable
	case PaymentIntentStatusSucceeded:
		return WebhookPaymentIntentSucceeded
	case PaymentIntentStatusCanceled:
		return WebhookPaymentIntentCanceled
	default:
		return ""
	}
}

func validatePaymentMethod(paymentMethodType string, card Card) error {
	if paymentMethodType != "card" {
		return stripeInvalid("parameter_invalid_enum", "type", fmt.Sprintf("PaymentMethod type %q is not supported by the Stagehand Stripe simulator.", paymentMethodType))
	}
	if card.Last4 != "" && (len(card.Last4) != 4 || !asciiDigitsOnly(card.Last4)) {
		return stripeInvalid("parameter_invalid_string", "card[last4]", "Card last4 must contain exactly four digits.")
	}
	if card.ExpMonth < 0 || card.ExpMonth > 12 {
		return stripeInvalid("parameter_invalid_integer", "card[exp_month]", "Card exp_month must be between 1 and 12.")
	}
	if card.ExpMonth == 0 && card.ExpYear != 0 {
		return stripeInvalid("parameter_missing", "card[exp_month]", "Card exp_month is required when exp_year is set.")
	}
	return nil
}

func asciiDigitsOnly(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func operationTime(unixSeconds int64) time.Time {
	if unixSeconds == 0 {
		return time.Time{}
	}
	return time.Unix(unixSeconds, 0).UTC()
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneStateMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func unescapePathPart(value string) string {
	unescaped, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return unescaped
}

func objectNotFound(kind string, id string) error {
	id = strings.TrimSpace(id)
	return &Error{
		Type:       "invalid_request_error",
		Code:       "resource_missing",
		Param:      kind,
		Message:    fmt.Sprintf("No such %s: %q.", kind, id),
		StatusCode: 404,
		cause:      ErrNotFound,
	}
}

func invalidRequest(message string) error {
	return stripeInvalid("invalid_request", "", message)
}

func stripeInvalid(code string, param string, message string) error {
	return &Error{
		Type:       "invalid_request_error",
		Code:       code,
		Param:      param,
		Message:    message,
		StatusCode: 400,
		cause:      ErrInvalidRequest,
	}
}

func injectedStripeError(override injection.ResponseOverride) error {
	errorType := "api_error"
	code := "stagehand_injected_error"
	param := ""
	message := "Injected Stagehand Stripe error."

	if errorBody, ok := override.Body["error"].(map[string]any); ok {
		errorType = stringFromAny(errorBody["type"], errorType)
		code = stringFromAny(errorBody["code"], code)
		param = stringFromAny(errorBody["param"], param)
		message = stringFromAny(errorBody["message"], message)
	}

	return &Error{
		Type:       errorType,
		Code:       code,
		Param:      param,
		Message:    message,
		StatusCode: override.Status,
		cause:      ErrInjected,
	}
}

func stringFromAny(value any, fallback string) string {
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}
