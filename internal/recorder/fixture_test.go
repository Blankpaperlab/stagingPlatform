package recorder

import (
	"strings"
	"testing"
)

func TestArtifactFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		path         string
		wantErrParts []string
	}{
		{
			name: "valid artifact fixture",
			path: "testdata/valid-run.json",
		},
		{
			name: "valid response_received artifact fixture",
			path: "testdata/valid-response-received-run.json",
		},
		{
			name: "valid timeout artifact fixture",
			path: "testdata/valid-timeout-run.json",
		},
		{
			name: "valid error artifact fixture",
			path: "testdata/valid-error-run.json",
		},
		{
			name: "valid streaming terminal artifact fixture",
			path: "testdata/valid-stream-terminal-run.json",
		},
		{
			name: "invalid artifact with unknown field",
			path: "testdata/invalid-unknown-field.json",
			wantErrParts: []string{
				"unknown field",
			},
		},
		{
			name: "invalid corrupted artifact without integrity issues",
			path: "testdata/invalid-corrupted-without-issues.json",
			wantErrParts: []string{
				"integrity_issues must contain at least one issue",
			},
		},
		{
			name: "invalid artifact with schema version mismatch",
			path: "testdata/invalid-schema-version.json",
			wantErrParts: []string{
				"schema_version must be",
			},
		},
		{
			name: "invalid artifact without terminal event",
			path: "testdata/invalid-missing-terminal-event.json",
			wantErrParts: []string{
				"events must end with a terminal event",
			},
		},
		{
			name: "invalid artifact with out of order interaction sequence",
			path: "testdata/invalid-out-of-order-interaction-sequence.json",
			wantErrParts: []string{
				"interactions[1].sequence must be strictly increasing",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(tc.path)
			assertArtifactFixtureResult(t, err, tc.wantErrParts)
		})
	}
}

func assertArtifactFixtureResult(t *testing.T, err error, wantErrParts []string) {
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
