# Stripe Simulator Subset

## Purpose

This document describes the Story `I1` and `I2` Stripe simulator slices.

The Go source of truth is:

- `internal/services/stripe/simulator.go`
- `internal/services/stripe/simulator_test.go`

## V1 Subset

The implemented object set is intentionally narrow:

- `customer`
- `payment_method`
- `payment_intent`

`payment_intent` was chosen instead of `charge` because modern Stripe integrations normally create and update PaymentIntents directly.

## Supported Request Matches

The simulator declares route-level matching for these Stripe API paths:

| Method | Path                              | Operation                  |
| ------ | --------------------------------- | -------------------------- |
| `POST` | `/v1/customers`                   | `customers.create`         |
| `GET`  | `/v1/customers/{id}`              | `customers.retrieve`       |
| `POST` | `/v1/customers/{id}`              | `customers.update`         |
| `POST` | `/v1/payment_methods`             | `payment_methods.create`   |
| `GET`  | `/v1/payment_methods/{id}`        | `payment_methods.retrieve` |
| `POST` | `/v1/payment_methods/{id}`        | `payment_methods.update`   |
| `POST` | `/v1/payment_methods/{id}/attach` | `payment_methods.attach`   |
| `POST` | `/v1/payment_intents`             | `payment_intents.create`   |
| `GET`  | `/v1/payment_intents/{id}`        | `payment_intents.retrieve` |
| `POST` | `/v1/payment_intents/{id}`        | `payment_intents.update`   |

## State Model

Stripe state is stored under the runtime session snapshot key `stripe`.

The session state contains:

- `customers`
- `payment_methods`
- `payment_intents`
- per-object counters for deterministic local IDs

Generated IDs are deterministic within a session snapshot lineage:

- customers: `cus_000001`
- payment methods: `pm_000001`
- payment intents: `pi_000001`

## Business Rules

The simulator now enforces the first set of Stripe-like consistency rules:

- payment methods can attach to one customer only
- payment intents cannot reference missing customers or payment methods
- payment methods attached to one customer cannot be used with a different customer
- payment intent amounts must be positive
- payment intent currencies are required and normalized to lowercase
- card payment methods are the only supported payment method type
- card `last4` must contain exactly four digits when provided

Supported PaymentIntent statuses:

- `requires_payment_method`
- `requires_confirmation`
- `requires_action`
- `requires_capture`
- `succeeded`
- `canceled`

Supported PaymentIntent transitions:

- `requires_payment_method` to `requires_confirmation` or `canceled`
- `requires_confirmation` to `requires_action`, `requires_capture`, `succeeded`, or `canceled`
- `requires_action` to `requires_confirmation` or `canceled`
- `requires_capture` to `succeeded` or `canceled`

Terminal statuses `succeeded` and `canceled` reject mutating updates to amount, currency, customer, payment method, or status.

Errors are returned as Stripe-shaped invalid-request errors with `type`, `code`, `param`, `message`, and `status_code` fields on the Go error value.

## Webhook Side Effects

When the simulator is backed by a store that also implements `EventQueueStore`, supported operations schedule push-mode webhook events through the runtime event queue.

Supported webhook topics:

- `webhook.customer.created`
- `webhook.customer.updated`
- `webhook.payment_method.attached`
- `webhook.payment_intent.created`
- `webhook.payment_intent.amount_capturable_updated`
- `webhook.payment_intent.succeeded`
- `webhook.payment_intent.canceled`

Each scheduled event payload uses Stripe's event envelope shape:

- `object: "event"`
- `type`
- `data.object`
- `customer_identity` when a customer can be resolved

## Customer Identity Extraction

`ExtractCustomerIdentity(session, customer_id)` returns a service-level customer identity projection:

- customer ID
- email
- name
- phone
- attached payment method IDs
- customer metadata

The extractor prefers direct customer email/name values and fills missing name/phone/email fields from attached payment method billing details.

## Current Limits

- No HTTP server adapter is exposed yet; this is the stateful simulator core.
- Named Stripe error injection is wired into the simulator core; injected failures return before state mutation and expose provenance for run metadata.
- Webhook events are scheduled in the local runtime queue, but no outbound webhook delivery adapter exists yet.

## Validation Coverage

Current tests cover:

- supported route matching
- unsupported route rejection
- customer create/read/update
- payment method create/read/update and attach
- payment intent create/read/update
- missing-reference failures
- payment method reattach rejection
- invalid PaymentIntent transition rejection
- terminal PaymentIntent mutation rejection
- Stripe-shaped error details
- scheduled webhook events for supported flows
- customer identity extraction
- persistence through session snapshots
- session isolation
