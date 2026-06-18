package gates

import (
	"fmt"

	analysisassertions "stagehand/internal/analysis/assertions"
	"stagehand/internal/recorder"
)

type EvaluationOptions struct {
	BaseRun *recorder.Run
}

type EvaluationSummary struct {
	Total       int `json:"total"`
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Unsupported int `json:"unsupported"`
}

type EvaluationResult struct {
	Status     analysisassertions.ResultStatus `json:"status"`
	Summary    EvaluationSummary               `json:"summary"`
	Results    []Result                        `json:"results"`
	Assertions analysisassertions.File         `json:"-"`
}

type Result struct {
	GateName    string                          `json:"gate_name"`
	GateType    string                          `json:"gate_type"`
	AssertionID string                          `json:"assertion_id"`
	Status      analysisassertions.ResultStatus `json:"status"`
	Message     string                          `json:"message"`
	Evidence    analysisassertions.Evidence     `json:"evidence"`
}

type translation struct {
	GateName string
	GateType string
}

func Evaluate(candidate recorder.Run, file File, opts EvaluationOptions) (EvaluationResult, error) {
	if err := file.Validate(); err != nil {
		return EvaluationResult{}, err
	}

	assertionFile, translations := Translate(file)
	assertionResults, err := analysisassertions.EvaluateWithOptions(candidate, assertionFile, analysisassertions.Options{
		BaseRun: opts.BaseRun,
	})
	if err != nil {
		return EvaluationResult{}, err
	}

	result := EvaluationResult{
		Status:     analysisassertions.ResultStatusPassed,
		Assertions: assertionFile,
		Results:    make([]Result, 0, len(assertionResults)),
	}
	for _, assertionResult := range assertionResults {
		translated := translations[assertionResult.AssertionID]
		gateResult := Result{
			GateName:    translated.GateName,
			GateType:    translated.GateType,
			AssertionID: assertionResult.AssertionID,
			Status:      assertionResult.Status,
			Message:     gateMessage(translated, assertionResult),
			Evidence:    assertionResult.Evidence,
		}
		result.Results = append(result.Results, gateResult)
		result.Summary.Total++
		switch gateResult.Status {
		case analysisassertions.ResultStatusPassed:
			result.Summary.Passed++
		case analysisassertions.ResultStatusFailed:
			result.Summary.Failed++
			result.Status = analysisassertions.ResultStatusFailed
		case analysisassertions.ResultStatusUnsupported:
			result.Summary.Unsupported++
			result.Status = analysisassertions.ResultStatusFailed
		}
	}

	return result, nil
}

func Translate(file File) (analysisassertions.File, map[string]translation) {
	assertionFile := analysisassertions.File{
		SchemaVersion: analysisassertions.SchemaVersion,
		Assertions:    make([]analysisassertions.Assertion, 0, len(file.ReleaseGates)),
	}
	translations := map[string]translation{}
	for idx, gate := range file.ReleaseGates {
		assertion := assertionForGate(idx, gate)
		assertionFile.Assertions = append(assertionFile.Assertions, assertion)
		translations[assertion.ID] = translation{
			GateName: gate.Name,
			GateType: gateType(gate),
		}
	}
	return assertionFile, translations
}

func assertionForGate(idx int, gate Gate) analysisassertions.Assertion {
	id := fmt.Sprintf("gate-%03d", idx+1)
	switch {
	case gate.BlockIf != nil:
		match := matchFromSelector(gate.BlockIf.ActionSelector)
		match.AmountGT = cloneFloat64(gate.BlockIf.AmountGT)
		if gate.BlockIf.ApprovalMissing {
			match.ApprovalMissing = boolPtr(true)
		}
		match.Channel = gate.BlockIf.Channel
		match.Domain = gate.BlockIf.Domain
		match.NewAction = cloneBool(gate.BlockIf.NewAction)
		match.UnknownRisk = cloneBool(gate.BlockIf.UnknownRisk)
		return blockingAssertion(id, match)
	case gate.RequireOrder != nil:
		return analysisassertions.Assertion{
			ID:     id,
			Type:   analysisassertions.TypeOrdering,
			Before: matchPtr(matchFromSelector(gate.RequireOrder.Before)),
			After:  matchPtr(matchFromSelector(gate.RequireOrder.After)),
		}
	case gate.RequireApproval != nil:
		match := matchFromSelector(gate.RequireApproval.ActionSelector)
		match.AmountGT = cloneFloat64(gate.RequireApproval.AmountGT)
		match.ApprovalMissing = boolPtr(true)
		return blockingAssertion(id, match)
	case gate.MaxAmount != nil:
		match := matchFromSelector(gate.MaxAmount.ActionSelector)
		match.AmountGT = cloneFloat64(gate.MaxAmount.Amount)
		return blockingAssertion(id, match)
	case gate.AllowedChannels != nil:
		match := matchFromSelector(gate.AllowedChannels.ActionSelector)
		match.ChannelNotIn = append([]string(nil), gate.AllowedChannels.Values...)
		return blockingAssertion(id, match)
	case gate.AllowedDomains != nil:
		match := matchFromSelector(gate.AllowedDomains.ActionSelector)
		match.DomainNotIn = append([]string(nil), gate.AllowedDomains.Values...)
		return blockingAssertion(id, match)
	case gate.ForbidNewAction != nil:
		match := matchFromSelector(*gate.ForbidNewAction)
		match.NewAction = boolPtr(true)
		return blockingAssertion(id, match)
	case gate.ForbidUnknownRisk != nil:
		match := matchFromSelector(gate.ForbidUnknownRisk.ActionSelector)
		match.UnknownRisk = boolPtr(true)
		return blockingAssertion(id, match)
	default:
		return blockingAssertion(id, analysisassertions.Match{InteractionID: "__invalid_gate__"})
	}
}

func blockingAssertion(id string, match analysisassertions.Match) analysisassertions.Assertion {
	max := 0
	return analysisassertions.Assertion{
		ID:    id,
		Type:  analysisassertions.TypeCount,
		Match: match,
		Expect: analysisassertions.Expect{
			Count: analysisassertions.CountExpectation{Max: &max},
		},
	}
}

func matchFromSelector(selector ActionSelector) analysisassertions.Match {
	return analysisassertions.Match{
		Service:         selector.Service,
		Operation:       selector.Operation,
		Tool:            selector.Tool,
		SideEffect:      string(selector.SideEffect),
		DestinationType: selector.DestinationType,
	}
}

func matchPtr(match analysisassertions.Match) *analysisassertions.Match {
	return &match
}

func boolPtr(value bool) *bool {
	return &value
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func gateType(gate Gate) string {
	switch {
	case gate.BlockIf != nil:
		return "block_if"
	case gate.RequireOrder != nil:
		return "require_order"
	case gate.RequireApproval != nil:
		return "require_approval"
	case gate.MaxAmount != nil:
		return "max_amount"
	case gate.AllowedChannels != nil:
		return "allowed_channels"
	case gate.AllowedDomains != nil:
		return "allowed_domains"
	case gate.ForbidNewAction != nil:
		return "forbid_new_action"
	case gate.ForbidUnknownRisk != nil:
		return "forbid_unknown_risk"
	default:
		return "unknown"
	}
}

func gateMessage(translated translation, result analysisassertions.Result) string {
	switch result.Status {
	case analysisassertions.ResultStatusPassed:
		return fmt.Sprintf("gate %q passed", translated.GateName)
	case analysisassertions.ResultStatusFailed:
		if result.Type == analysisassertions.TypeOrdering {
			return fmt.Sprintf("gate %q failed: required order was not observed", translated.GateName)
		}
		return fmt.Sprintf("gate %q failed: %d matching interaction(s) violated the gate", translated.GateName, result.Evidence.Count)
	default:
		return fmt.Sprintf("gate %q unsupported: %s", translated.GateName, result.Message)
	}
}

func AllPassed(result EvaluationResult) bool {
	return result.Summary.Failed == 0 && result.Summary.Unsupported == 0
}
