// Package conformance defines simulator conformance case files and result records.
package conformance

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "v1alpha1"

type MatchStrategy string

const (
	MatchStrategyExactSequence       MatchStrategy = "exact_sequence"
	MatchStrategyOperationSequence   MatchStrategy = "operation_sequence"
	MatchStrategyInteractionIdentity MatchStrategy = "interaction_identity"
)

type ResultStatus string

const (
	ResultStatusPassed  ResultStatus = "passed"
	ResultStatusFailed  ResultStatus = "failed"
	ResultStatusSkipped ResultStatus = "skipped"
	ResultStatusError   ResultStatus = "error"
)

var validMatchStrategies = []MatchStrategy{
	MatchStrategyExactSequence,
	MatchStrategyOperationSequence,
	MatchStrategyInteractionIdentity,
}

type CaseFile struct {
	SchemaVersion string `yaml:"schema_version"`
	Cases         []Case `yaml:"cases"`
}

type Case struct {
	ID          string                  `yaml:"id"`
	Service     string                  `yaml:"service"`
	Description string                  `yaml:"description"`
	Tags        []string                `yaml:"tags"`
	Inputs      CaseInputs              `yaml:"inputs"`
	RealService RealServiceRequirements `yaml:"real_service"`
	Comparison  ComparisonConfig        `yaml:"comparison"`
}

type CaseInputs struct {
	Scenario string     `yaml:"scenario"`
	Steps    []CaseStep `yaml:"steps"`
}

type CaseStep struct {
	ID        string         `yaml:"id"`
	Operation string         `yaml:"operation"`
	Request   map[string]any `yaml:"request"`
	Expect    map[string]any `yaml:"expect"`
}

type RealServiceRequirements struct {
	Network      bool                    `yaml:"network"`
	TestMode     bool                    `yaml:"test_mode"`
	Credentials  []CredentialRequirement `yaml:"credentials"`
	CostEstimate string                  `yaml:"cost_estimate"`
}

type CredentialRequirement struct {
	Env      string `yaml:"env"`
	Required bool   `yaml:"required"`
	Purpose  string `yaml:"purpose"`
}

type ComparisonConfig struct {
	Match               MatchConfig          `yaml:"match"`
	ToleratedDiffFields []ToleratedDiffField `yaml:"tolerated_diff_fields"`
}

type MatchConfig struct {
	Strategy MatchStrategy `yaml:"strategy"`
	Keys     []string      `yaml:"keys"`
}

type ToleratedDiffField struct {
	Path      string   `yaml:"path"`
	Reason    string   `yaml:"reason"`
	AppliesTo []string `yaml:"applies_to"`
}

type Result struct {
	CaseID       string        `json:"case_id"`
	Service      string        `json:"service"`
	Status       ResultStatus  `json:"status"`
	RealRun      RunReference  `json:"real_run"`
	SimulatorRun RunReference  `json:"simulator_run"`
	Summary      ResultSummary `json:"summary"`
	Matches      []MatchResult `json:"matches"`
	Failures     []Failure     `json:"failures"`
}

type RunReference struct {
	RunID       string `json:"run_id,omitempty"`
	SessionName string `json:"session_name,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

type ResultSummary struct {
	MatchedInteractions int `json:"matched_interactions"`
	FailingDiffs        int `json:"failing_diffs"`
	ToleratedDiffs      int `json:"tolerated_diffs"`
	MissingInteractions int `json:"missing_interactions"`
	ExtraInteractions   int `json:"extra_interactions"`
}

type MatchResult struct {
	StepID                 string   `json:"step_id,omitempty"`
	RealInteractionID      string   `json:"real_interaction_id,omitempty"`
	SimulatorInteractionID string   `json:"simulator_interaction_id,omitempty"`
	Operation              string   `json:"operation,omitempty"`
	ToleratedFields        []string `json:"tolerated_fields,omitempty"`
}

type Failure struct {
	Code                   string `json:"code"`
	Message                string `json:"message"`
	StepID                 string `json:"step_id,omitempty"`
	Path                   string `json:"path,omitempty"`
	RealInteractionID      string `json:"real_interaction_id,omitempty"`
	SimulatorInteractionID string `json:"simulator_interaction_id,omitempty"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid conformance case file"
	}
	return fmt.Sprintf("invalid conformance case file: %s", strings.Join(e.Problems, "; "))
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

func Load(path string) (CaseFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CaseFile{}, fmt.Errorf("read conformance cases %q: %w", path, err)
	}
	return Parse(data)
}

func Parse(data []byte) (CaseFile, error) {
	var file CaseFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return CaseFile{}, fmt.Errorf("decode conformance cases: %w", err)
	}
	if err := file.Validate(); err != nil {
		return CaseFile{}, err
	}
	return file, nil
}

func (f CaseFile) Validate() error {
	verr := &ValidationError{}
	if f.SchemaVersion != SchemaVersion {
		verr.add("schema_version must be %q", SchemaVersion)
	}
	if len(f.Cases) == 0 {
		verr.add("cases must contain at least one case")
	}

	seenIDs := map[string]bool{}
	for idx, testCase := range f.Cases {
		prefix := fmt.Sprintf("cases[%d]", idx)
		validateCase(prefix, testCase, seenIDs, verr)
	}
	return verr.err()
}

func validateCase(prefix string, testCase Case, seenIDs map[string]bool, verr *ValidationError) {
	if strings.TrimSpace(testCase.ID) == "" {
		verr.add("%s.id is required", prefix)
	} else if seenIDs[testCase.ID] {
		verr.add("%s.id %q is duplicated", prefix, testCase.ID)
	}
	seenIDs[testCase.ID] = true

	if strings.TrimSpace(testCase.Service) == "" {
		verr.add("%s.service is required", prefix)
	}
	if len(testCase.Inputs.Steps) == 0 {
		verr.add("%s.inputs.steps must contain at least one step", prefix)
	}
	for stepIdx, step := range testCase.Inputs.Steps {
		validateStep(fmt.Sprintf("%s.inputs.steps[%d]", prefix, stepIdx), step, verr)
	}
	for credentialIdx, credential := range testCase.RealService.Credentials {
		validateCredential(fmt.Sprintf("%s.real_service.credentials[%d]", prefix, credentialIdx), credential, verr)
	}
	validateComparison(prefix+".comparison", testCase.Comparison, verr)
}

func validateStep(prefix string, step CaseStep, verr *ValidationError) {
	if strings.TrimSpace(step.ID) == "" {
		verr.add("%s.id is required", prefix)
	}
	if strings.TrimSpace(step.Operation) == "" {
		verr.add("%s.operation is required", prefix)
	}
}

func validateCredential(prefix string, credential CredentialRequirement, verr *ValidationError) {
	if strings.TrimSpace(credential.Env) == "" {
		verr.add("%s.env is required", prefix)
	}
	if credential.Required && strings.TrimSpace(credential.Purpose) == "" {
		verr.add("%s.purpose is required when credential is required", prefix)
	}
}

func validateComparison(prefix string, comparison ComparisonConfig, verr *ValidationError) {
	if comparison.Match.Strategy == "" {
		verr.add("%s.match.strategy is required", prefix)
	} else if !slices.Contains(validMatchStrategies, comparison.Match.Strategy) {
		verr.add("%s.match.strategy must be one of %q, %q, or %q", prefix, MatchStrategyExactSequence, MatchStrategyOperationSequence, MatchStrategyInteractionIdentity)
	}
	if len(comparison.Match.Keys) == 0 && comparison.Match.Strategy == MatchStrategyInteractionIdentity {
		verr.add("%s.match.keys must contain at least one key for %q", prefix, MatchStrategyInteractionIdentity)
	}
	for idx, field := range comparison.ToleratedDiffFields {
		fieldPrefix := fmt.Sprintf("%s.tolerated_diff_fields[%d]", prefix, idx)
		if strings.TrimSpace(field.Path) == "" {
			verr.add("%s.path is required", fieldPrefix)
		}
		if strings.TrimSpace(field.Reason) == "" {
			verr.add("%s.reason is required", fieldPrefix)
		}
	}
}
