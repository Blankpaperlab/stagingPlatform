package gates

import (
	"strings"
	"testing"
)

func TestReleaseGateFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		path         string
		wantErrParts []string
	}{
		{
			name: "valid release gates",
			path: "testdata/valid.stagehand.gates.yml",
		},
		{
			name: "invalid unknown field",
			path: "testdata/invalid-unknown-field.stagehand.gates.yml",
			wantErrParts: []string{
				"field typo not found",
			},
		},
		{
			name: "invalid schema version",
			path: "testdata/invalid-schema-version.stagehand.gates.yml",
			wantErrParts: []string{
				"schema_version must be",
			},
		},
		{
			name: "invalid gate clause",
			path: "testdata/invalid-gate-clause.stagehand.gates.yml",
			wantErrParts: []string{
				"must set exactly one gate clause",
			},
		},
		{
			name: "invalid values",
			path: "testdata/invalid-values.stagehand.gates.yml",
			wantErrParts: []string{
				"values contains duplicate",
				"values[2] cannot be empty",
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
