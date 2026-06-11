package contracts

import "fmt"

type RiskLevel string

const (
	RiskLevelLow     RiskLevel = "low"
	RiskLevelMedium  RiskLevel = "medium"
	RiskLevelHigh    RiskLevel = "high"
	RiskLevelBlocked RiskLevel = "blocked"
)

type RiskAssessment struct {
	Level   RiskLevel `json:"level"`
	Reasons []string  `json:"reasons,omitempty"`
}

// ScoreRisk assigns an overall risk level for a review based on the contract-level
// behavior diff between a base and candidate run and the contract evaluation result
// for the candidate run.
//
// Precedence: a contract violation always blocks, regardless of what changed;
// otherwise the highest-risk new action observed determines the level.
func ScoreRisk(diff DiffResult, evaluation EvaluationResult) RiskAssessment {
	if evaluation.Summary.Total > 0 {
		return RiskAssessment{
			Level:   RiskLevelBlocked,
			Reasons: []string{fmt.Sprintf("%d contract violation(s) must be resolved before this run can be approved", evaluation.Summary.Total)},
		}
	}

	var highReasons, mediumReasons []string
	for _, change := range diff.Changes {
		if change.Type != DiffChangeNewAction {
			continue
		}
		label := diffActionLabel(change)
		switch change.CandidateSideEffect {
		case SideEffectFinancial, SideEffectDestructive, SideEffectExternalMessage:
			highReasons = append(highReasons, fmt.Sprintf("new %s action %s was not present in the base run", change.CandidateSideEffect, label))
		case SideEffectWrite:
			mediumReasons = append(mediumReasons, fmt.Sprintf("new write action %s was not present in the base run", label))
		}
	}

	if len(highReasons) > 0 {
		return RiskAssessment{Level: RiskLevelHigh, Reasons: highReasons}
	}
	if len(mediumReasons) > 0 {
		return RiskAssessment{Level: RiskLevelMedium, Reasons: mediumReasons}
	}
	if diff.Summary.NewActions > 0 {
		return RiskAssessment{Level: RiskLevelLow, Reasons: []string{"new actions observed are read-only"}}
	}
	return RiskAssessment{Level: RiskLevelLow, Reasons: []string{"no new actions observed; changes are read-only or absent"}}
}
