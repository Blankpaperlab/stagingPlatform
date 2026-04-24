package scrub

import (
	"fmt"
	"strings"
)

func MergeRules(defaults []Rule, custom []Rule) ([]Rule, error) {
	verr := &ValidationError{}

	defaultsByPattern := make(map[string]Rule, len(defaults))
	defaultsByName := make(map[string]Rule, len(defaults))
	for _, rule := range defaults {
		normalized := normalizeRulePattern(rule.Pattern)
		rule.Pattern = normalized
		defaultsByPattern[normalized] = rule
		if strings.TrimSpace(rule.Name) != "" {
			defaultsByName[rule.Name] = rule
		}
	}

	seenCustomNames := map[string]int{}
	seenCustomPatterns := map[string]int{}
	for idx, rule := range custom {
		rule.Pattern = normalizeRulePattern(rule.Pattern)
		custom[idx] = rule

		if strings.TrimSpace(rule.Name) == "" {
			verr.add("custom rules[%d].name is required", idx)
		}
		if strings.TrimSpace(rule.Pattern) == "" {
			verr.add("custom rules[%d].pattern is required", idx)
		}
		if !isValidAction(rule.Action) {
			verr.add("custom rules[%d].action must be one of %q, %q, %q, %q", idx, ActionDrop, ActionMask, ActionHash, ActionPreserve)
		}

		if existing, ok := defaultsByName[rule.Name]; ok {
			verr.add("custom rules[%d].name %q conflicts with built-in rule %q", idx, rule.Name, existing.Name)
		}

		if existing, ok := defaultsByPattern[rule.Pattern]; ok {
			verr.add("custom rules[%d].pattern %q conflicts with built-in rule %q", idx, rule.Pattern, existing.Name)
		}

		if previous, ok := seenCustomNames[rule.Name]; ok && strings.TrimSpace(rule.Name) != "" {
			verr.add("custom rules[%d].name %q duplicates custom rules[%d]", idx, rule.Name, previous)
		} else if strings.TrimSpace(rule.Name) != "" {
			seenCustomNames[rule.Name] = idx
		}

		if previous, ok := seenCustomPatterns[rule.Pattern]; ok && strings.TrimSpace(rule.Pattern) != "" {
			verr.add("custom rules[%d].pattern %q duplicates custom rules[%d]", idx, rule.Pattern, previous)
		} else if strings.TrimSpace(rule.Pattern) != "" {
			seenCustomPatterns[rule.Pattern] = idx
		}
	}

	if err := verr.err(); err != nil {
		return nil, err
	}

	merged := make([]Rule, 0, len(defaults)+len(custom))
	merged = append(merged, defaults...)
	merged = append(merged, custom...)
	return merged, nil
}

func CloneRules(rules []Rule) []Rule {
	cloned := make([]Rule, len(rules))
	copy(cloned, rules)
	return cloned
}

func MustMergeRules(defaults []Rule, custom []Rule) []Rule {
	merged, err := MergeRules(defaults, custom)
	if err != nil {
		panic(fmt.Sprintf("invalid scrub rules: %v", err))
	}

	return merged
}

func normalizeRulePattern(pattern string) string {
	trimmed := strings.TrimSpace(pattern)
	lowered := strings.ToLower(trimmed)
	for _, prefix := range []string{"request.headers.", "response.headers."} {
		if strings.HasPrefix(lowered, prefix) {
			return prefix + lowered[len(prefix):]
		}
	}
	return trimmed
}
