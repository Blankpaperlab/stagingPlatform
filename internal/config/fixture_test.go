package config

import (
	"strings"
	"testing"
)

func TestRuntimeConfigFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		path         string
		wantErrParts []string
	}{
		{
			name: "valid minimal runtime config",
			path: "testdata/runtime-valid-minimal.stagehand.yml",
		},
		{
			name: "invalid runtime config with unknown field",
			path: "testdata/runtime-invalid-unknown-field.stagehand.yml",
			wantErrParts: []string{
				"field unexpected_field not found",
			},
		},
		{
			name: "invalid runtime config with schema version mismatch",
			path: "testdata/runtime-invalid-schema-version.stagehand.yml",
			wantErrParts: []string{
				"schema_version must be",
			},
		},
		{
			name: "valid runtime config with custom scrub rules",
			path: "testdata/runtime-valid-custom-rules.stagehand.yml",
		},
		{
			name: "invalid runtime config with conflicting custom scrub rule",
			path: "testdata/runtime-invalid-custom-rule-conflict.stagehand.yml",
			wantErrParts: []string{
				"conflicts with built-in rule",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(tc.path)
			assertFixtureResult(t, err, tc.wantErrParts)
		})
	}
}

func TestTestConfigFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		path         string
		wantErrParts []string
	}{
		{
			name: "valid minimal test config",
			path: "testdata/test-valid-minimal.stagehand.test.yml",
		},
		{
			name: "invalid replay conflict test config",
			path: "testdata/test-invalid-replay-conflict.stagehand.test.yml",
			wantErrParts: []string{
				"replay.forbid_live_network",
			},
		},
		{
			name: "invalid duplicate fallback tiers",
			path: "testdata/test-invalid-duplicate-fallback.stagehand.test.yml",
			wantErrParts: []string{
				"fallback.disallowed_tiers contains duplicate tier",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := LoadTest(tc.path)
			assertFixtureResult(t, err, tc.wantErrParts)
		})
	}
}

func assertFixtureResult(t *testing.T, err error, wantErrParts []string) {
	t.Helper()

	if len(wantErrParts) == 0 {
		if err != nil {
			t.Fatalf("expected fixture to be valid, got %v", err)
		}
		return
	}

	if err == nil {
		t.Fatal("expected fixture to be invalid")
	}

	for _, want := range wantErrParts {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
}
