// Package assertions parses and validates Stagehand assertion files.
package assertions

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "v1alpha1"

type AssertionType string

const (
	TypeCount               AssertionType = "count"
	TypeOrdering            AssertionType = "ordering"
	TypePayloadField        AssertionType = "payload-field"
	TypeForbiddenOperation  AssertionType = "forbidden-operation"
	TypeFallbackProhibition AssertionType = "fallback-prohibition"
	TypeCrossService        AssertionType = "cross-service"
)

type Relationship string

const (
	RelationshipEquals Relationship = "equals"
)

var (
	validAssertionTypes = []AssertionType{
		TypeCount,
		TypeOrdering,
		TypePayloadField,
		TypeForbiddenOperation,
		TypeFallbackProhibition,
		TypeCrossService,
	}
	validRelationships = []Relationship{
		RelationshipEquals,
	}
)

type File struct {
	SchemaVersion string      `yaml:"schema_version"`
	Assertions    []Assertion `yaml:"assertions"`
}

type Assertion struct {
	ID           string        `yaml:"id"`
	Type         AssertionType `yaml:"type"`
	Description  string        `yaml:"description"`
	Match        Match         `yaml:"match"`
	Expect       Expect        `yaml:"expect"`
	Before       *Match        `yaml:"before"`
	After        *Match        `yaml:"after"`
	Left         *EntityRef    `yaml:"left"`
	Right        *EntityRef    `yaml:"right"`
	Relationship Relationship  `yaml:"relationship"`
}

type Match struct {
	Service       string `yaml:"service"`
	Operation     string `yaml:"operation"`
	InteractionID string `yaml:"interaction_id"`
	EventType     string `yaml:"event_type"`
	FallbackTier  string `yaml:"fallback_tier"`
}

type EntityRef struct {
	Service   string `yaml:"service"`
	Operation string `yaml:"operation"`
	Path      string `yaml:"path"`
}

type Expect struct {
	Count           CountExpectation `yaml:"count"`
	Path            string           `yaml:"path"`
	Equals          any              `yaml:"equals"`
	NotEquals       any              `yaml:"not_equals"`
	Exists          *bool            `yaml:"exists"`
	DisallowedTiers []string         `yaml:"disallowed_tiers"`
}

type CountExpectation struct {
	Equals *int `yaml:"equals"`
	Min    *int `yaml:"min"`
	Max    *int `yaml:"max"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid assertion file"
	}
	return fmt.Sprintf("invalid assertion file: %s", strings.Join(e.Problems, "; "))
}

func (e *ValidationError) add(format string, args ...any) {
	e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
}

func (e *ValidationError) err() error {
	if len(e.Problems) == 0 {
		return nil
	}
	return e
}

func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read assertions %q: %w", path, err)
	}
	return Parse(data)
}

func Parse(data []byte) (File, error) {
	var file File
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return File{}, fmt.Errorf("decode assertions: %w", err)
	}
	if err := file.Validate(); err != nil {
		return File{}, err
	}
	return file, nil
}

func (f File) Validate() error {
	verr := &ValidationError{}

	if f.SchemaVersion != SchemaVersion {
		verr.add("schema_version must be %q", SchemaVersion)
	}
	if len(f.Assertions) == 0 {
		verr.add("assertions must contain at least one assertion")
	}

	seenIDs := map[string]bool{}
	for idx, assertion := range f.Assertions {
		prefix := fmt.Sprintf("assertions[%d]", idx)
		if strings.TrimSpace(assertion.ID) == "" {
			verr.add("%s.id is required", prefix)
		} else if seenIDs[assertion.ID] {
			verr.add("%s.id %q is duplicated", prefix, assertion.ID)
		}
		seenIDs[assertion.ID] = true

		if !slices.Contains(validAssertionTypes, assertion.Type) {
			verr.add("%s.type must be one of %q, %q, %q, %q, %q, %q", prefix, TypeCount, TypeOrdering, TypePayloadField, TypeForbiddenOperation, TypeFallbackProhibition, TypeCrossService)
			continue
		}
		validateAssertion(prefix, assertion, verr)
	}

	return verr.err()
}

func validateAssertion(prefix string, assertion Assertion, verr *ValidationError) {
	switch assertion.Type {
	case TypeCount:
		validateCount(prefix, assertion, verr)
	case TypeOrdering:
		validateOrdering(prefix, assertion, verr)
	case TypePayloadField:
		validatePayloadField(prefix, assertion, verr)
	case TypeForbiddenOperation:
		validateForbiddenOperation(prefix, assertion, verr)
	case TypeFallbackProhibition:
		validateFallbackProhibition(prefix, assertion, verr)
	case TypeCrossService:
		validateCrossService(prefix, assertion, verr)
	}
}

func validateCount(prefix string, assertion Assertion, verr *ValidationError) {
	validateMatch(prefix+".match", assertion.Match, verr)
	expect := assertion.Expect.Count
	hasEquals := expect.Equals != nil
	hasRange := expect.Min != nil || expect.Max != nil
	if !hasEquals && !hasRange {
		verr.add("%s.expect.count must set equals, min, or max", prefix)
	}
	if hasEquals && hasRange {
		verr.add("%s.expect.count cannot combine equals with min or max", prefix)
	}
	validateNonNegativeInt(prefix+".expect.count.equals", expect.Equals, verr)
	validateNonNegativeInt(prefix+".expect.count.min", expect.Min, verr)
	validateNonNegativeInt(prefix+".expect.count.max", expect.Max, verr)
	if expect.Min != nil && expect.Max != nil && *expect.Min > *expect.Max {
		verr.add("%s.expect.count.min cannot be greater than max", prefix)
	}
	rejectOrderingFields(prefix, assertion, verr)
	rejectCrossServiceFields(prefix, assertion, verr)
}

func validateOrdering(prefix string, assertion Assertion, verr *ValidationError) {
	if assertion.Before == nil {
		verr.add("%s.before is required for ordering assertions", prefix)
	} else {
		validateMatch(prefix+".before", *assertion.Before, verr)
	}
	if assertion.After == nil {
		verr.add("%s.after is required for ordering assertions", prefix)
	} else {
		validateMatch(prefix+".after", *assertion.After, verr)
	}
	if hasMatch(assertion.Match) {
		verr.add("%s.match is not allowed for ordering assertions; use before and after", prefix)
	}
	rejectExpectFields(prefix, assertion.Expect, verr)
	rejectCrossServiceFields(prefix, assertion, verr)
}

func validatePayloadField(prefix string, assertion Assertion, verr *ValidationError) {
	validateMatch(prefix+".match", assertion.Match, verr)
	if strings.TrimSpace(assertion.Expect.Path) == "" {
		verr.add("%s.expect.path is required for payload-field assertions", prefix)
	} else if !validPath(assertion.Expect.Path) {
		verr.add("%s.expect.path is malformed", prefix)
	}
	comparators := 0
	if assertion.Expect.Equals != nil {
		comparators++
	}
	if assertion.Expect.NotEquals != nil {
		comparators++
	}
	if assertion.Expect.Exists != nil {
		comparators++
	}
	if comparators != 1 {
		verr.add("%s.expect must set exactly one of equals, not_equals, or exists", prefix)
	}
	if hasCountExpectation(assertion.Expect.Count) {
		verr.add("%s.expect.count is only valid for count assertions", prefix)
	}
	if len(assertion.Expect.DisallowedTiers) > 0 {
		verr.add("%s.expect.disallowed_tiers is only valid for fallback-prohibition assertions", prefix)
	}
	rejectOrderingFields(prefix, assertion, verr)
	rejectCrossServiceFields(prefix, assertion, verr)
}

func validateForbiddenOperation(prefix string, assertion Assertion, verr *ValidationError) {
	if strings.TrimSpace(assertion.Match.Service) == "" {
		verr.add("%s.match.service is required for forbidden-operation assertions", prefix)
	}
	if strings.TrimSpace(assertion.Match.Operation) == "" {
		verr.add("%s.match.operation is required for forbidden-operation assertions", prefix)
	}
	rejectExpectFields(prefix, assertion.Expect, verr)
	rejectOrderingFields(prefix, assertion, verr)
	rejectCrossServiceFields(prefix, assertion, verr)
}

func validateFallbackProhibition(prefix string, assertion Assertion, verr *ValidationError) {
	validateMatch(prefix+".match", assertion.Match, verr)
	if len(assertion.Expect.DisallowedTiers) == 0 {
		verr.add("%s.expect.disallowed_tiers must contain at least one fallback tier", prefix)
	}
	for idx, tier := range assertion.Expect.DisallowedTiers {
		if strings.TrimSpace(tier) == "" {
			verr.add("%s.expect.disallowed_tiers[%d] cannot be empty", prefix, idx)
		}
	}
	if hasCountExpectation(assertion.Expect.Count) {
		verr.add("%s.expect.count is only valid for count assertions", prefix)
	}
	if strings.TrimSpace(assertion.Expect.Path) != "" || assertion.Expect.Equals != nil || assertion.Expect.NotEquals != nil || assertion.Expect.Exists != nil {
		verr.add("%s.expect may only set disallowed_tiers for fallback-prohibition assertions", prefix)
	}
	rejectOrderingFields(prefix, assertion, verr)
	rejectCrossServiceFields(prefix, assertion, verr)
}

func validateCrossService(prefix string, assertion Assertion, verr *ValidationError) {
	if assertion.Left == nil {
		verr.add("%s.left is required for cross-service assertions", prefix)
	} else {
		validateEntityRef(prefix+".left", *assertion.Left, verr)
	}
	if assertion.Right == nil {
		verr.add("%s.right is required for cross-service assertions", prefix)
	} else {
		validateEntityRef(prefix+".right", *assertion.Right, verr)
	}
	if !slices.Contains(validRelationships, assertion.Relationship) {
		verr.add("%s.relationship must be %q", prefix, RelationshipEquals)
	}
	if hasMatch(assertion.Match) {
		verr.add("%s.match is not allowed for cross-service assertions; use left and right", prefix)
	}
	rejectExpectFields(prefix, assertion.Expect, verr)
	rejectOrderingFields(prefix, assertion, verr)
}

func validateMatch(prefix string, match Match, verr *ValidationError) {
	if !hasMatch(match) {
		verr.add("%s must set at least one selector field", prefix)
	}
}

func validateEntityRef(prefix string, ref EntityRef, verr *ValidationError) {
	if strings.TrimSpace(ref.Service) == "" {
		verr.add("%s.service is required", prefix)
	}
	if strings.TrimSpace(ref.Operation) == "" {
		verr.add("%s.operation is required", prefix)
	}
	if strings.TrimSpace(ref.Path) == "" {
		verr.add("%s.path is required", prefix)
	} else if !validPath(ref.Path) {
		verr.add("%s.path is malformed", prefix)
	}
}

func validateNonNegativeInt(path string, value *int, verr *ValidationError) {
	if value != nil && *value < 0 {
		verr.add("%s must be greater than or equal to 0", path)
	}
}

func rejectExpectFields(prefix string, expect Expect, verr *ValidationError) {
	if hasCountExpectation(expect.Count) || strings.TrimSpace(expect.Path) != "" || expect.Equals != nil || expect.NotEquals != nil || expect.Exists != nil || len(expect.DisallowedTiers) > 0 {
		verr.add("%s.expect is not allowed for this assertion type", prefix)
	}
}

func rejectOrderingFields(prefix string, assertion Assertion, verr *ValidationError) {
	if assertion.Before != nil || assertion.After != nil {
		verr.add("%s.before and %s.after are only valid for ordering assertions", prefix, prefix)
	}
}

func rejectCrossServiceFields(prefix string, assertion Assertion, verr *ValidationError) {
	if assertion.Left != nil || assertion.Right != nil || assertion.Relationship != "" {
		verr.add("%s.left, %s.right, and %s.relationship are only valid for cross-service assertions", prefix, prefix, prefix)
	}
}

func hasMatch(match Match) bool {
	return strings.TrimSpace(match.Service) != "" ||
		strings.TrimSpace(match.Operation) != "" ||
		strings.TrimSpace(match.InteractionID) != "" ||
		strings.TrimSpace(match.EventType) != "" ||
		strings.TrimSpace(match.FallbackTier) != ""
}

func hasCountExpectation(expect CountExpectation) bool {
	return expect.Equals != nil || expect.Min != nil || expect.Max != nil
}

func validPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, segment := range strings.Split(path, ".") {
		if strings.TrimSpace(segment) == "" {
			return false
		}
	}
	return true
}
