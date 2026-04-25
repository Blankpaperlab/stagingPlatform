// Package stripe implements the first stateful Stripe simulator slice.
package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"stagehand/internal/runtime/session"
	"stagehand/internal/store"
)

const (
	stateKey = "stripe"

	Service = "stripe"

	OperationCustomersCreate        = "customers.create"
	OperationCustomersRetrieve      = "customers.retrieve"
	OperationCustomersUpdate        = "customers.update"
	OperationPaymentMethodsCreate   = "payment_methods.create"
	OperationPaymentMethodsRetrieve = "payment_methods.retrieve"
	OperationPaymentMethodsUpdate   = "payment_methods.update"
	OperationPaymentMethodsAttach   = "payment_methods.attach"
	OperationPaymentIntentsCreate   = "payment_intents.create"
	OperationPaymentIntentsRetrieve = "payment_intents.retrieve"
	OperationPaymentIntentsUpdate   = "payment_intents.update"
)

var (
	ErrInvalidRequest = errors.New("stripe invalid request")
	ErrNotFound       = errors.New("stripe object not found")
)

type Simulator struct {
	store    store.SessionStore
	sessions *session.Manager
	now      func() time.Time
	mu       sync.Mutex
}

type Option func(*simulatorOptions)

type simulatorOptions struct {
	now           func() time.Time
	snapshotNewID func(prefix string) (string, error)
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

func NewSimulator(sessionStore store.SessionStore, opts ...Option) (*Simulator, error) {
	if sessionStore == nil {
		return nil, fmt.Errorf("stripe simulator session store is required")
	}

	options := simulatorOptions{
		now: func() time.Time {
			return time.Now().UTC()
		},
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

	return &Simulator{
		store:    sessionStore,
		sessions: sessionManager,
		now:      options.now,
	}, nil
}

func (s *Simulator) CreateSession(ctx context.Context, sessionName string) (store.SessionRecord, error) {
	return s.sessions.Create(ctx, sessionName)
}

type State struct {
	Customers      map[string]Customer      `json:"customers"`
	PaymentMethods map[string]PaymentMethod `json:"payment_methods"`
	PaymentIntents map[string]PaymentIntent `json:"payment_intents"`
	Counters       Counters                 `json:"counters"`
}

type Counters struct {
	Customer      int `json:"customer"`
	PaymentMethod int `json:"payment_method"`
	PaymentIntent int `json:"payment_intent"`
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
	Currency        string            `json:"currency"`
	CustomerID      string            `json:"customer,omitempty"`
	PaymentMethodID string            `json:"payment_method,omitempty"`
	Status          string            `json:"status"`
	ClientSecret    string            `json:"client_secret"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Created         int64             `json:"created"`
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
	case normalizedMethod == "GET" && len(parts) == 3 && parts[1] == "payment_intents":
		match.Operation = OperationPaymentIntentsRetrieve
		match.Params["payment_intent_id"] = unescapePathPart(parts[2])
		return match, true
	case normalizedMethod == "POST" && len(parts) == 3 && parts[1] == "payment_intents":
		match.Operation = OperationPaymentIntentsUpdate
		match.Params["payment_intent_id"] = unescapePathPart(parts[2])
		return match, true
	default:
		return Match{}, false
	}
}

func (s *Simulator) CreateCustomer(ctx context.Context, sessionName string, params CreateCustomerParams) (Customer, error) {
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
	return customer, nil
}

func (s *Simulator) GetCustomer(ctx context.Context, sessionName string, customerID string) (Customer, error) {
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

func (s *Simulator) UpdateCustomer(ctx context.Context, sessionName string, customerID string, params UpdateCustomerParams) (Customer, error) {
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
	return customer, nil
}

func (s *Simulator) CreatePaymentMethod(ctx context.Context, sessionName string, params CreatePaymentMethodParams) (PaymentMethod, error) {
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
	paymentMethod.CustomerID = customerID

	state.PaymentMethods[paymentMethod.ID] = paymentMethod
	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentMethod{}, err
	}
	return paymentMethod, nil
}

func (s *Simulator) CreatePaymentIntent(ctx context.Context, sessionName string, params CreatePaymentIntentParams) (PaymentIntent, error) {
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
		return PaymentIntent{}, invalidRequest("payment_intent amount must be greater than 0")
	}

	currency := normalizeCurrency(params.Currency)
	if currency == "" {
		return PaymentIntent{}, invalidRequest("payment_intent currency is required")
	}

	state.Counters.PaymentIntent++
	paymentIntent := PaymentIntent{
		ID:              fmt.Sprintf("pi_%06d", state.Counters.PaymentIntent),
		Object:          "payment_intent",
		Amount:          params.Amount,
		Currency:        currency,
		CustomerID:      strings.TrimSpace(params.CustomerID),
		PaymentMethodID: strings.TrimSpace(params.PaymentMethodID),
		Status:          normalizePaymentIntentStatus(params.Status, params.PaymentMethodID),
		Metadata:        cloneStringMap(params.Metadata),
		Created:         s.now().Unix(),
	}
	paymentIntent.ClientSecret = paymentIntent.ID + "_secret_stagehand"
	state.PaymentIntents[paymentIntent.ID] = paymentIntent

	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentIntent{}, err
	}
	return paymentIntent, nil
}

func (s *Simulator) GetPaymentIntent(ctx context.Context, sessionName string, paymentIntentID string) (PaymentIntent, error) {
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

func (s *Simulator) UpdatePaymentIntent(ctx context.Context, sessionName string, paymentIntentID string, params UpdatePaymentIntentParams) (PaymentIntent, error) {
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

	if params.Amount != nil {
		if *params.Amount <= 0 {
			return PaymentIntent{}, invalidRequest("payment_intent amount must be greater than 0")
		}
		paymentIntent.Amount = *params.Amount
	}
	if params.Currency != nil {
		currency := normalizeCurrency(*params.Currency)
		if currency == "" {
			return PaymentIntent{}, invalidRequest("payment_intent currency is required")
		}
		paymentIntent.Currency = currency
	}
	if params.CustomerID != nil {
		customerID := strings.TrimSpace(*params.CustomerID)
		if customerID != "" {
			if _, ok := state.Customers[customerID]; !ok {
				return PaymentIntent{}, objectNotFound("customer", customerID)
			}
		}
		paymentIntent.CustomerID = customerID
	}
	if params.PaymentMethodID != nil {
		paymentMethodID := strings.TrimSpace(*params.PaymentMethodID)
		if paymentMethodID != "" {
			if _, ok := state.PaymentMethods[paymentMethodID]; !ok {
				return PaymentIntent{}, objectNotFound("payment_method", paymentMethodID)
			}
		}
		paymentIntent.PaymentMethodID = paymentMethodID
	}
	if params.Status != nil {
		paymentIntent.Status = normalizePaymentIntentStatus(*params.Status, paymentIntent.PaymentMethodID)
	}
	if params.Metadata != nil {
		paymentIntent.Metadata = cloneStringMap(params.Metadata)
	}

	state.PaymentIntents[paymentIntent.ID] = paymentIntent
	if err := s.saveState(ctx, sessionName, sessionState, state); err != nil {
		return PaymentIntent{}, err
	}
	return paymentIntent, nil
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

func normalizeCurrency(currency string) string {
	return strings.ToLower(strings.TrimSpace(currency))
}

func normalizePaymentIntentStatus(status string, paymentMethodID string) string {
	status = strings.TrimSpace(status)
	if status != "" {
		return status
	}
	if strings.TrimSpace(paymentMethodID) != "" {
		return "requires_confirmation"
	}
	return "requires_payment_method"
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
	return fmt.Errorf("%w: %s %q", ErrNotFound, kind, strings.TrimSpace(id))
}

func invalidRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, message)
}
