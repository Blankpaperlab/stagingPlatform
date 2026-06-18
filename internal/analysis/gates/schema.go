// Package gates parses and validates Stagehand release gate files.
package gates

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"

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

var validSideEffects = []SideEffect{
	SideEffectRead,
	SideEffectWrite,
	SideEffectDestructive,
	SideEffectFinancial,
	SideEffectExternalMessage,
	SideEffectUnknown,
}

type File struct {
	SchemaVersion string `yaml:"schema_version"`
	ReleaseGates  []Gate `yaml:"release_gates"`
}

type Gate struct {
	Name              string             `yaml:"name"`
	BlockIf           *BlockIf           `yaml:"block_if"`
	RequireOrder      *RequireOrder      `yaml:"require_order"`
	RequireApproval   *RequireApproval   `yaml:"require_approval"`
	MaxAmount         *MaxAmount         `yaml:"max_amount"`
	AllowedChannels   *AllowedValues     `yaml:"allowed_channels"`
	AllowedDomains    *AllowedValues     `yaml:"allowed_domains"`
	ForbidNewAction   *ActionSelector    `yaml:"forbid_new_action"`
	ForbidUnknownRisk *ForbidUnknownRisk `yaml:"forbid_unknown_risk"`
}

type ActionSelector struct {
	Service         string     `yaml:"service"`
	Operation       string     `yaml:"operation"`
	Tool            string     `yaml:"tool"`
	SideEffect      SideEffect `yaml:"side_effect"`
	DestinationType string     `yaml:"destination_type"`
}

type BlockIf struct {
	ActionSelector  `yaml:",inline"`
	AmountGT        *float64 `yaml:"amount_gt"`
	ApprovalMissing bool     `yaml:"approval_missing"`
	Channel         string   `yaml:"channel"`
	Domain          string   `yaml:"domain"`
	NewAction       *bool    `yaml:"new_action"`
	UnknownRisk     *bool    `yaml:"unknown_risk"`
}

type RequireOrder struct {
	Before ActionSelector `yaml:"before"`
	After  ActionSelector `yaml:"after"`
}

type RequireApproval struct {
	ActionSelector `yaml:",inline"`
	AmountGT       *float64 `yaml:"amount_gt"`
}

type MaxAmount struct {
	ActionSelector `yaml:",inline"`
	Amount         *float64 `yaml:"amount"`
}

type AllowedValues struct {
	ActionSelector `yaml:",inline"`
	Values         []string `yaml:"values"`
}

type ForbidUnknownRisk struct {
	ActionSelector `yaml:",inline"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid release gate file"
	}

	return fmt.Sprintf("invalid release gate file: %s", strings.Join(e.Problems, "; "))
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
		return File{}, fmt.Errorf("read release gates %q: %w", path, err)
	}

	return Parse(data)
}

func Parse(data []byte) (File, error) {
	var file File
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		return File{}, fmt.Errorf("decode release gates: %w", err)
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
	if len(f.ReleaseGates) == 0 {
		verr.add("release_gates must contain at least one gate")
	}

	for idx, gate := range f.ReleaseGates {
		validateGate(fmt.Sprintf("release_gates[%d]", idx), gate, verr)
	}

	return verr.err()
}

func validateGate(prefix string, gate Gate, verr *ValidationError) {
	if strings.TrimSpace(gate.Name) == "" {
		verr.add("%s.name is required", prefix)
	}

	clauses := 0
	countClause := func(set bool) {
		if set {
			clauses++
		}
	}
	countClause(gate.BlockIf != nil)
	countClause(gate.RequireOrder != nil)
	countClause(gate.RequireApproval != nil)
	countClause(gate.MaxAmount != nil)
	countClause(gate.AllowedChannels != nil)
	countClause(gate.AllowedDomains != nil)
	countClause(gate.ForbidNewAction != nil)
	countClause(gate.ForbidUnknownRisk != nil)

	if clauses != 1 {
		verr.add("%s must set exactly one gate clause", prefix)
	}

	if gate.BlockIf != nil {
		validateBlockIf(prefix+".block_if", *gate.BlockIf, verr)
	}
	if gate.RequireOrder != nil {
		validateRequireOrder(prefix+".require_order", *gate.RequireOrder, verr)
	}
	if gate.RequireApproval != nil {
		validateRequireApproval(prefix+".require_approval", *gate.RequireApproval, verr)
	}
	if gate.MaxAmount != nil {
		validateMaxAmount(prefix+".max_amount", *gate.MaxAmount, verr)
	}
	if gate.AllowedChannels != nil {
		validateAllowedValues(prefix+".allowed_channels", *gate.AllowedChannels, verr)
	}
	if gate.AllowedDomains != nil {
		validateAllowedValues(prefix+".allowed_domains", *gate.AllowedDomains, verr)
	}
	if gate.ForbidNewAction != nil {
		validateSelector(prefix+".forbid_new_action", *gate.ForbidNewAction, false, verr)
	}
	if gate.ForbidUnknownRisk != nil {
		validateSelector(prefix+".forbid_unknown_risk", gate.ForbidUnknownRisk.ActionSelector, false, verr)
	}
}

func validateBlockIf(prefix string, block BlockIf, verr *ValidationError) {
	validateSelector(prefix, block.ActionSelector, false, verr)
	if block.AmountGT != nil && *block.AmountGT < 0 {
		verr.add("%s.amount_gt must be greater than or equal to 0", prefix)
	}
	if !hasSelector(block.ActionSelector) &&
		block.AmountGT == nil &&
		!block.ApprovalMissing &&
		strings.TrimSpace(block.Channel) == "" &&
		strings.TrimSpace(block.Domain) == "" &&
		block.NewAction == nil &&
		block.UnknownRisk == nil {
		verr.add("%s must set at least one condition field", prefix)
	}
}

func validateRequireOrder(prefix string, order RequireOrder, verr *ValidationError) {
	validateSelector(prefix+".before", order.Before, true, verr)
	validateSelector(prefix+".after", order.After, true, verr)
}

func validateRequireApproval(prefix string, approval RequireApproval, verr *ValidationError) {
	validateSelector(prefix, approval.ActionSelector, false, verr)
	if !hasSelector(approval.ActionSelector) {
		verr.add("%s must set at least one selector field", prefix)
	}
	if approval.AmountGT != nil && *approval.AmountGT < 0 {
		verr.add("%s.amount_gt must be greater than or equal to 0", prefix)
	}
}

func validateMaxAmount(prefix string, max MaxAmount, verr *ValidationError) {
	validateSelector(prefix, max.ActionSelector, false, verr)
	if !hasSelector(max.ActionSelector) {
		verr.add("%s must set at least one selector field", prefix)
	}
	if max.Amount == nil {
		verr.add("%s.amount is required", prefix)
	} else if *max.Amount < 0 {
		verr.add("%s.amount must be greater than or equal to 0", prefix)
	}
}

func validateAllowedValues(prefix string, allowed AllowedValues, verr *ValidationError) {
	validateSelector(prefix, allowed.ActionSelector, false, verr)
	validateStringSet(prefix+".values", allowed.Values, verr)
	if len(allowed.Values) == 0 {
		verr.add("%s.values must contain at least one value", prefix)
	}
}

func validateSelector(prefix string, selector ActionSelector, requireAction bool, verr *ValidationError) {
	service := strings.TrimSpace(selector.Service)
	operation := strings.TrimSpace(selector.Operation)
	tool := strings.TrimSpace(selector.Tool)

	if tool != "" && (service != "" || operation != "") {
		verr.add("%s.tool cannot be combined with service or operation", prefix)
	}
	if operation != "" && service == "" && tool == "" {
		verr.add("%s.service is required when operation is set", prefix)
	}
	if requireAction && service == "" && tool == "" {
		verr.add("%s must set service or tool", prefix)
	}
	validateSideEffect(prefix+".side_effect", selector.SideEffect, verr)
}

func validateSideEffect(field string, sideEffect SideEffect, verr *ValidationError) {
	if sideEffect == "" {
		return
	}
	if !slices.Contains(validSideEffects, sideEffect) {
		verr.add("%s must be one of %q, %q, %q, %q, %q, %q", field, SideEffectRead, SideEffectWrite, SideEffectDestructive, SideEffectFinancial, SideEffectExternalMessage, SideEffectUnknown)
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

func hasSelector(selector ActionSelector) bool {
	return strings.TrimSpace(selector.Service) != "" ||
		strings.TrimSpace(selector.Operation) != "" ||
		strings.TrimSpace(selector.Tool) != "" ||
		selector.SideEffect != "" ||
		strings.TrimSpace(selector.DestinationType) != ""
}
