package scrub

import (
	"slices"
	"strings"
	"testing"
)

func TestMergeRulesAppendsCustomRulesAfterDefaults(t *testing.T) {
	t.Parallel()

	custom := []Rule{
		{
			Name:    "customer-email-mask",
			Pattern: "request.body.customer.email",
			Action:  ActionMask,
		},
		{
			Name:    "token-hash",
			Pattern: "request.query.token",
			Action:  ActionHash,
		},
	}

	merged, err := MergeRules(DefaultRules(), custom)
	if err != nil {
		t.Fatalf("MergeRules() error = %v", err)
	}

	wantNames := []string{
		"request-authorization-header",
		"request-cookie-header",
		"customer-email-mask",
		"token-hash",
	}
	gotNames := make([]string, len(merged))
	for idx, rule := range merged {
		gotNames[idx] = rule.Name
	}

	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("merged rule order = %#v, want %#v", gotNames, wantNames)
	}
}

func TestMergeRulesRejectsCustomConflictWithBuiltInPattern(t *testing.T) {
	t.Parallel()

	_, err := MergeRules(DefaultRules(), []Rule{
		{
			Name:    "override-auth-header",
			Pattern: "request.headers.authorization",
			Action:  ActionPreserve,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with built-in rule") {
		t.Fatalf("MergeRules() error = %v, want built-in conflict error", err)
	}
}

func TestMergeRulesRejectsDuplicateCustomPattern(t *testing.T) {
	t.Parallel()

	_, err := MergeRules(DefaultRules(), []Rule{
		{
			Name:    "first",
			Pattern: "request.body.customer.email",
			Action:  ActionMask,
		},
		{
			Name:    "second",
			Pattern: "request.body.customer.email",
			Action:  ActionHash,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicates custom rules") {
		t.Fatalf("MergeRules() error = %v, want duplicate custom pattern error", err)
	}
}
