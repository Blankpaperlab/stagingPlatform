// Package contracts parses and validates Stagehand agent behavior contracts.
package contracts

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = "v1alpha1"

type SideEffect string

const (
	SideEffectRead            SideEffect = "read"
	SideEffectWrite           SideEffect = "write"
	SideEffectDestructive     SideEffect = "destructive"
	SideEffectFinancial       SideEffect = "financial"
	SideEffectExternalMessage SideEffect = "external_message"
	SideEffectUnknown         SideEffect = "unknown"
)

type FallbackTier string

const (
	FallbackTierExact           FallbackTier = "exact"
	FallbackTierNearestNeighbor FallbackTier = "nearest_neighbor"
	FallbackTierStateSynthesis  FallbackTier = "state_synthesis"
	FallbackTierLLMSynthesis    FallbackTier = "llm_synthesis"
)

type ApprovalSource string

const (
	ApprovalSourceHuman    ApprovalSource = "human"
	ApprovalSourceSystem   ApprovalSource = "system"
	ApprovalSourceExternal ApprovalSource = "external"
)

var (
	validSideEffects = []SideEffect{
		SideEffectRead,
		SideEffectWrite,
		SideEffectDestructive,
		SideEffectFinancial,
		SideEffectExternalMessage,
		SideEffectUnknown,
	}
	validFallbackTiers = []FallbackTier{
		FallbackTierExact,
		FallbackTierNearestNeighbor,
		FallbackTierStateSynthesis,
		FallbackTierLLMSynthesis,
	}
	validApprovalSources = []ApprovalSource{
		ApprovalSourceHuman,
		ApprovalSourceSystem,
		ApprovalSourceExternal,
	}
)

type File struct {
	SchemaVersion     string         `yaml:"schema_version"`
	Agent             Agent          `yaml:"agent"`
	AllowedActions    []Action       `yaml:"allowed_actions"`
	RestrictedActions []Action       `yaml:"restricted_actions"`
	ForbiddenActions  []Action       `yaml:"forbidden_actions"`
	UserOverrides     []UserOverride `yaml:"user_overrides"`
}

type Agent struct {
	Name   string   `yaml:"name"`
	Models []string `yaml:"models"`
}

type Action struct {
	Service              string               `yaml:"service"`
	Operation            string               `yaml:"operation"`
	Tool                 string               `yaml:"tool"`
	SideEffect           SideEffect           `yaml:"side_effect"`
	ClassifierReason     string               `yaml:"classifier_reason"`
	AllowedFallbackTiers []FallbackTier       `yaml:"allowed_fallback_tiers"`
	RequiresApproval     bool                 `yaml:"requires_approval"`
	Approval             *ApprovalRequirement `yaml:"approval"`
	MaxAmount            *float64             `yaml:"max_amount"`
	Reason               string               `yaml:"reason"`
}

type ApprovalRequirement struct {
	Source       ApprovalSource `yaml:"source"`
	Role         string         `yaml:"role"`
	EvidencePath string         `yaml:"evidence_path"`
}

type UserOverride struct {
	ID                   string         `yaml:"id"`
	Match                ActionSelector `yaml:"match"`
	SideEffect           SideEffect     `yaml:"side_effect"`
	AllowedFallbackTiers []FallbackTier `yaml:"allowed_fallback_tiers"`
	RequiresApproval     *bool          `yaml:"requires_approval"`
	MaxAmount            *float64       `yaml:"max_amount"`
	Reason               string         `yaml:"reason"`
	ApprovedBy           string         `yaml:"approved_by"`
	ApprovedAt           string         `yaml:"approved_at"`
}

type ActionSelector struct {
	Service   string `yaml:"service"`
	Operation string `yaml:"operation"`
	Tool      string `yaml:"tool"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid behavior contract"
	}

	return fmt.Sprintf("invalid behavior contract: %s", strings.Join(e.Problems, "; "))
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
		return File{}, fmt.Errorf("read behavior contract %q: %w", path, err)
	}

	return Parse(data)
}

func Parse(data []byte) (File, error) {
	var file File
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return File{}, fmt.Errorf("decode behavior contract: %w", err)
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
	if strings.TrimSpace(f.Agent.Name) == "" {
		verr.add("agent.name is required")
	}
	validateStringSet("agent.models", f.Agent.Models, verr)
	if len(f.AllowedActions)+len(f.RestrictedActions)+len(f.ForbiddenActions) == 0 {
		verr.add("at least one action is required")
	}

	seenSelectors := map[string]string{}
	validateActionList("allowed_actions", f.AllowedActions, false, seenSelectors, verr)
	validateActionList("restricted_actions", f.RestrictedActions, false, seenSelectors, verr)
	validateActionList("forbidden_actions", f.ForbiddenActions, true, seenSelectors, verr)
	validateUserOverrides(f.UserOverrides, verr)

	return verr.err()
}

func validateActionList(field string, actions []Action, requireReason bool, seen map[string]string, verr *ValidationError) {
	for idx, action := range actions {
		prefix := fmt.Sprintf("%s[%d]", field, idx)
		selectorKey, ok := validateActionSelector(prefix, ActionSelector{
			Service:   action.Service,
			Operation: action.Operation,
			Tool:      action.Tool,
		}, verr)
		if ok {
			if previous, exists := seen[selectorKey]; exists {
				verr.add("%s duplicates action selector already defined at %s", prefix, previous)
			} else {
				seen[selectorKey] = prefix
			}
		}

		validateSideEffect(prefix+".side_effect", action.SideEffect, true, verr)
		validateFallbackTiers(prefix+".allowed_fallback_tiers", action.AllowedFallbackTiers, verr)
		validateMaxAmount(prefix+".max_amount", action.MaxAmount, verr)
		validateApproval(prefix, action.RequiresApproval, action.Approval, verr)

		if requireReason && strings.TrimSpace(action.Reason) == "" {
			verr.add("%s.reason is required for forbidden actions", prefix)
		}
	}
}

func validateUserOverrides(overrides []UserOverride, verr *ValidationError) {
	seenIDs := map[string]bool{}
	for idx, override := range overrides {
		prefix := fmt.Sprintf("user_overrides[%d]", idx)
		id := strings.TrimSpace(override.ID)
		if id == "" {
			verr.add("%s.id is required", prefix)
		} else if seenIDs[id] {
			verr.add("%s.id %q is duplicated", prefix, id)
		}
		seenIDs[id] = true

		validateActionSelector(prefix+".match", override.Match, verr)
		validateSideEffect(prefix+".side_effect", override.SideEffect, false, verr)
		validateFallbackTiers(prefix+".allowed_fallback_tiers", override.AllowedFallbackTiers, verr)
		validateMaxAmount(prefix+".max_amount", override.MaxAmount, verr)

		if override.SideEffect == "" && len(override.AllowedFallbackTiers) == 0 && override.RequiresApproval == nil && override.MaxAmount == nil {
			verr.add("%s must set at least one override field", prefix)
		}
		if strings.TrimSpace(override.Reason) == "" {
			verr.add("%s.reason is required", prefix)
		}
		if strings.TrimSpace(override.ApprovedAt) != "" {
			if _, err := time.Parse(time.RFC3339, override.ApprovedAt); err != nil {
				verr.add("%s.approved_at must be an RFC3339 timestamp", prefix)
			}
		}
	}
}

func validateActionSelector(prefix string, selector ActionSelector, verr *ValidationError) (string, bool) {
	service := strings.TrimSpace(selector.Service)
	operation := strings.TrimSpace(selector.Operation)
	tool := strings.TrimSpace(selector.Tool)

	if tool != "" {
		if service != "" || operation != "" {
			verr.add("%s.tool cannot be combined with service or operation", prefix)
			return "", false
		}

		return "tool:" + tool, true
	}

	if service == "" {
		verr.add("%s.service is required when tool is not set", prefix)
	}
	if operation == "" {
		verr.add("%s.operation is required when tool is not set", prefix)
	}
	if service == "" || operation == "" {
		return "", false
	}

	return "service:" + service + "\noperation:" + operation, true
}

func validateSideEffect(field string, sideEffect SideEffect, required bool, verr *ValidationError) {
	if sideEffect == "" {
		if required {
			verr.add("%s is required", field)
		}
		return
	}
	if !slices.Contains(validSideEffects, sideEffect) {
		verr.add("%s must be one of %q, %q, %q, %q, %q, %q", field, SideEffectRead, SideEffectWrite, SideEffectDestructive, SideEffectFinancial, SideEffectExternalMessage, SideEffectUnknown)
	}
}

func validateFallbackTiers(field string, tiers []FallbackTier, verr *ValidationError) {
	seen := map[FallbackTier]bool{}
	lastIndex := -1
	for idx, tier := range tiers {
		index := slices.Index(validFallbackTiers, tier)
		if index == -1 {
			verr.add("%s[%d] contains invalid fallback tier %q", field, idx, tier)
			continue
		}
		if seen[tier] {
			verr.add("%s contains duplicate fallback tier %q", field, tier)
			continue
		}
		seen[tier] = true
		if index < lastIndex {
			verr.add("%s must keep tiers in canonical order", field)
			continue
		}
		lastIndex = index
	}
}

func validateMaxAmount(field string, value *float64, verr *ValidationError) {
	if value != nil && *value < 0 {
		verr.add("%s must be greater than or equal to 0", field)
	}
}

func validateStringSet(field string, values []string, verr *ValidationError) {
	seen := map[string]bool{}
	for idx, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			verr.add("%s[%d] cannot be empty", field, idx)
			continue
		}
		if seen[trimmed] {
			verr.add("%s contains duplicate value %q", field, trimmed)
			continue
		}
		seen[trimmed] = true
	}
}

func validateApproval(prefix string, requiresApproval bool, approval *ApprovalRequirement, verr *ValidationError) {
	if approval == nil {
		return
	}
	if !requiresApproval {
		verr.add("%s.approval cannot be set unless requires_approval is true", prefix)
	}
	if !slices.Contains(validApprovalSources, approval.Source) {
		verr.add("%s.approval.source must be one of %q, %q, %q", prefix, ApprovalSourceHuman, ApprovalSourceSystem, ApprovalSourceExternal)
	}
}
