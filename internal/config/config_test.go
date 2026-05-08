package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := DefaultConfig()

	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig() should be valid, got %v", err)
	}
}

func TestDefaultTestConfigRequiresSession(t *testing.T) {
	cfg := DefaultTestConfig()

	err := cfg.Validate()
	if err == nil {
		t.Fatal("DefaultTestConfig() should require a session value")
	}

	if !strings.Contains(err.Error(), "session is required") {
		t.Fatalf("expected session validation error, got %v", err)
	}
}

func TestLoadConfigAppliesDefaultsAndValidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
record:
  storage_path: .stagehand/runs
auth:
  service_modes:
    stripe: strict-recorded
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Record.DefaultMode != ModeRecord {
		t.Fatalf("Record.DefaultMode = %q, want %q", cfg.Record.DefaultMode, ModeRecord)
	}

	if cfg.Replay.Mode != ReplayModeExact {
		t.Fatalf("Replay.Mode = %q, want %q", cfg.Replay.Mode, ReplayModeExact)
	}
}

func TestLoadConfigSupportsServiceMappings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.acme.com
      path_prefix: /v1
    replay:
      mode: generic_http
      allowed_tiers: [0, 1]
    ignore:
      request_paths:
        - headers.x-request-id
        - query.cursor
        - body.request_id
      response_paths:
        - body.generated_at
        - body.trace_id
  - name: billing-api
    type: api
    match:
      host: billing.internal.acme.com
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Services) != 2 {
		t.Fatalf("len(Services) = %d, want 2", len(cfg.Services))
	}
	if cfg.Services[0].Name != "internal-crm" {
		t.Fatalf("Services[0].Name = %q, want internal-crm", cfg.Services[0].Name)
	}
	if cfg.Services[0].Match.Host != "crm.internal.acme.com" {
		t.Fatalf("Services[0].Match.Host = %q, want crm.internal.acme.com", cfg.Services[0].Match.Host)
	}
	if cfg.Services[0].Replay.Mode != "generic_http" {
		t.Fatalf("Services[0].Replay.Mode = %q, want generic_http", cfg.Services[0].Replay.Mode)
	}
	if got := cfg.Services[0].Ignore.RequestPaths; len(got) != 3 || got[0] != "headers.x-request-id" || got[1] != "query.cursor" || got[2] != "body.request_id" {
		t.Fatalf("Services[0].Ignore.RequestPaths = %#v, want configured request ignore paths", got)
	}
	if got := cfg.Services[0].Ignore.ResponsePaths; len(got) != 2 || got[0] != "body.generated_at" || got[1] != "body.trace_id" {
		t.Fatalf("Services[0].Ignore.ResponsePaths = %#v, want configured response ignore paths", got)
	}
}

func TestLoadRejectsInvalidServiceMappings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.acme.com
  - name: internal-crm
    type: queue
    match:
      path_prefix: v1
    replay:
      mode: synthetic
      allowed_tiers: [1, 0, 4]
    ignore:
      request_paths:
        - ""
        - body.request_id
        - body.request_id
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected service mapping validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"services[1].name duplicates",
		"services[1].type",
		"services[1].match.host",
		"services[1].match.path_prefix",
		"services[1].replay.mode",
		"services[1].replay.allowed_tiers",
		"services[1].ignore.request_paths",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestLoadRejectsServiceIgnoreWithWhitespaceOnlyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.acme.com
      path_prefix: /v1
    ignore:
      response_paths:
        - "   "
        - body.generated_at
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected validation failure for whitespace-only ignore path")
	}

	if !strings.Contains(err.Error(), "services[0].ignore.response_paths") {
		t.Fatalf("validation error %q missing from %v", "services[0].ignore.response_paths", err)
	}
}

func TestLoadAcceptsServiceMappingWithoutIgnoreSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
services:
  - name: internal-crm
    type: api
    match:
      host: crm.internal.acme.com
      path_prefix: /v1
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := cfg.Services[0].Ignore.RequestPaths; len(got) != 0 {
		t.Fatalf("Services[0].Ignore.RequestPaths = %#v, want empty when ignore section omitted", got)
	}
	if got := cfg.Services[0].Ignore.ResponsePaths; len(got) != 0 {
		t.Fatalf("Services[0].Ignore.ResponsePaths = %#v, want empty when ignore section omitted", got)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
record:
  default_mode: invalid
  storage_path: ""
  capture:
    max_body_bytes: 0
    redact_before_persist: false
fallback:
  allowed_tiers:
    - state_synthesis
    - exact
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"record.default_mode",
		"record.storage_path",
		"record.capture.max_body_bytes",
		"record.capture.redact_before_persist",
		"fallback.allowed_tiers",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestLoadRejectsDisabledScrubProtections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
scrub:
  enabled: false
  policy_version: v1
  detectors:
    email: false
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected scrub protection validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"scrub.enabled",
		"scrub.detectors.email",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestLoadConfigSupportsCustomScrubRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
scrub:
  policy_version: v1
  custom_rules:
    - name: customer-email-mask
      pattern: request.body.customer.email
      action: mask
    - name: support-id-preserve
      pattern: request.body.ticket.support_id
      action: preserve
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	rules, err := cfg.Scrub.Rules()
	if err != nil {
		t.Fatalf("Scrub.Rules() error = %v", err)
	}

	if len(rules) != 4 {
		t.Fatalf("len(Scrub.Rules()) = %d, want 4", len(rules))
	}

	if rules[2].Name != "customer-email-mask" || rules[3].Name != "support-id-preserve" {
		t.Fatalf("merged custom rule names = [%q, %q], want customer-email-mask/support-id-preserve", rules[2].Name, rules[3].Name)
	}
}

func TestLoadRejectsConflictingCustomScrubRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
scrub:
  policy_version: v1
  custom_rules:
    - name: override-auth
      pattern: request.headers.authorization
      action: preserve
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() expected validation failure")
	}

	if !strings.Contains(err.Error(), "conflicts with built-in rule") {
		t.Fatalf("Load() error = %v, want built-in conflict validation", err)
	}
}

func TestLoadConfigNormalizesMixedCaseHeaderScrubRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.yml")
	content := `
schema_version: v1alpha1
scrub:
  policy_version: v1
  custom_rules:
    - name: customer-email-header-mask
      pattern: request.headers.X-Customer-Email
      action: mask
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	rules, err := cfg.Scrub.Rules()
	if err != nil {
		t.Fatalf("Scrub.Rules() error = %v", err)
	}

	got := rules[len(rules)-1].Pattern
	if got != "request.headers.x-customer-email" {
		t.Fatalf("normalized header pattern = %q, want %q", got, "request.headers.x-customer-email")
	}
}

func TestLoadTestConfigAndValidateRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.test.yml")
	content := `
schema_version: v1alpha1
session: onboarding-flow
error_injection:
  enabled: true
  rules:
    - match:
        service: stripe
        operation: create_charge
        nth_call: 3
      inject:
        status: 402
        body:
          error:
            code: card_declined
ci:
  fail_on:
    - behavior_diff
    - assertion_failure
  report_format: github-markdown
  artifact_name: stagehand-run
  max_fallback_tier: nearest_neighbor
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadTest(path)
	if err != nil {
		t.Fatalf("LoadTest() error = %v", err)
	}

	if cfg.Session != "onboarding-flow" {
		t.Fatalf("Session = %q, want onboarding-flow", cfg.Session)
	}

	if cfg.CI.ReportFormat != ReportFormatGitHubMarkdown {
		t.Fatalf("CI.ReportFormat = %q, want %q", cfg.CI.ReportFormat, ReportFormatGitHubMarkdown)
	}
}

func TestLoadTestRejectsContradictoryReplayConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stagehand.test.yml")
	content := `
schema_version: v1alpha1
session: prompt-flow
replay:
  mode: prompt_replay
  forbid_live_network: true
ci:
  fail_on:
    - behavior_diff
  report_format: terminal
  artifact_name: stagehand-run
  max_fallback_tier: nearest_neighbor
`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadTest(path)
	if err == nil {
		t.Fatal("LoadTest() expected validation failure")
	}

	if !strings.Contains(err.Error(), "replay.forbid_live_network") {
		t.Fatalf("expected replay conflict validation error, got %v", err)
	}
}
