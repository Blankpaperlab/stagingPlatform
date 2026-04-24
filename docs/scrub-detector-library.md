# Detector Library

## Purpose

This document describes the current Stagehand detector library implemented for Story `E2`.

The Go source of truth is:

- `internal/scrub/detectors/detectors.go`
- `internal/scrub/detectors/corpus_test.go`
- `internal/scrub/detectors/testdata/corpus.json`

This package provides the free-text detector primitives consumed by the Go scrub pipeline.

It now:

- returns ordered detector matches
- feeds automatic payload mutation in the structural scrub pipeline
- supports detector-set selection from validated runtime config
- records detector categories in `scrub_report.detector_kinds` when detector-driven scrubbing changes an interaction

It still does not:

- drive Python-side capture directly
- provide exhaustive secret coverage beyond the current standard detector set

## Scope

The current library scans free text and returns ordered detector matches.

Each match reports:

- detector kind
- matched value
- start offset
- end offset

## Supported Detectors

`DefaultLibrary()` currently includes:

- email
- JWT
- phone number
- SSN
- Luhn-validated credit card
- common API key prefixes
- password-assignment text such as `Password: hunter2`

## Detector Rules

### Email

- matches standard address forms such as `alice@example.com`
- rejects malformed domains such as `alice@example`

### JWT

- requires three dot-separated base64url segments
- decodes header and payload
- requires JSON payloads and an `alg` field in the header
- avoids matching simple dotted version strings such as `1.2.3`

### Phone

- accepts roughly 10 to 15 digits with common separators
- supports forms such as `+1 (415) 555-2671`
- rejects short codes
- rejects phone-like digit runs embedded inside larger word-like tokens

### SSN

- accepts 9-digit SSN forms with or without dashes
- rejects invalid area/group/serial combinations such as `000-12-3456`

### Credit Card

- accepts 13 to 19 digits with spaces or hyphens
- requires a passing Luhn checksum

### API Key Prefixes

The default library currently recognizes common prefixed tokens including:

- `sk-...`
- `sk-proj-...`
- `sk_live_...`
- `sk_test_...`
- `pk_live_...`
- `pk_test_...`
- `rk_live_...`
- `rk_test_...`
- `AIza...`
- `ghp_...`

### Password Assignment

- detects short free-text password disclosures when a password-like label points at a token
- supports forms such as `Password: hunter2`, `passwd = hunter2`, and short explanatory labels ending in `: hunter2`
- intentionally does not match generic phrases such as `password reset`

## Library Behavior

`Library.Scan(text)`:

1. runs every configured detector
2. removes duplicate matches
3. sorts matches by byte offset

This gives the scrub pipeline a stable ordered match list for in-place replacement.

## Validation Coverage

Current tests cover:

- positive and negative corpus cases for every detector kind
- ordering of mixed detector matches by text offset
- deduplication of overlapping API-key prefix matches
- scrub-report detector kind emission through the structural scrub pipeline

The corpus lives in `internal/scrub/detectors/testdata/corpus.json`.

## Current Limits

Known limits in the current E2 implementation:

- API key coverage is prefix-based and intentionally incomplete
- phone detection is heuristic and will always have edge cases
- the detector library is implemented in Go and is not yet called from the Python capture path directly; Python durable capture still needs to route through the recorder-side persisted writer
