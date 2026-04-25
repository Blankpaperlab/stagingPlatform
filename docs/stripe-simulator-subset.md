# Stripe Simulator Subset

## Purpose

This document describes the Story `I1` Stripe simulator slice.

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

## Current Limits

- No HTTP server adapter is exposed yet; this is the stateful simulator core.
- No webhook side effects are scheduled yet; that belongs to Story `I2`.
- No named error injection is implemented yet; that belongs to Story `I3`.
- PaymentIntent state transitions are permissive in this slice. Realistic invalid-transition behavior belongs to Story `I2`.

## Validation Coverage

Current tests cover:

- supported route matching
- unsupported route rejection
- customer create/read/update
- payment method create/read/update and attach
- payment intent create/read/update
- missing-reference failures
- persistence through session snapshots
- session isolation
