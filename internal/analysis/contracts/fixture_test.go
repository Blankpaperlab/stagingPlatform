package contracts

import (
	"strings"
	"testing"
)

func TestBehaviorContractFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		path         string
		wantErrParts []string
	}{
		{
			name: "valid behavior contract",
			path: "testdata/valid.stagehand.contract.yml",
		},
		{
			name: "invalid unknown field",
			path: "testdata/invalid-unknown-field.stagehand.contract.yml",
			wantErrParts: []string{
				"field typo not found",
			},
		},
		{
			name: "invalid schema version",
			path: "testdata/invalid-schema-version.stagehand.contract.yml",
			wantErrParts: []string{
				"schema_version must be",
			},
		},
		{
			name: "invalid duplicate selector",
			path: "testdata/invalid-duplicate-selector.stagehand.contract.yml",
			wantErrParts: []string{
				"duplicates action selector",
			},
		},
		{
			name: "invalid user override",
			path: "testdata/invalid-user-override.stagehand.contract.yml",
			wantErrParts: []string{
				"user_overrides[0].reason is required",
				"user_overrides[0].approved_at",
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
