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
