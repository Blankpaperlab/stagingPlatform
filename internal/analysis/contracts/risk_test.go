package contracts

import "testing"

func TestScoreRiskBlockedOnContractViolations(t *testing.T) {
	t.Parallel()

	diff := DiffResult{Changes: []DiffChange{
		{Type: DiffChangeNewAction, Service: "stripe", Operation: "POST /v1/refunds", CandidateSideEffect: SideEffectFinancial},
	}}
	evaluation := EvaluationResult{Status: EvaluationStatusFailed, Summary: EvaluationSummary{Total: 1, Forbidden: 1}}

	got := ScoreRisk(diff, evaluation)
	if got.Level != RiskLevelBlocked {
		t.Fatalf("Level = %q, want %q", got.Level, RiskLevelBlocked)
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("expected an explanation for the blocked level")
	}
}

func TestScoreRiskHighOnNewFinancialAction(t *testing.T) {
	t.Parallel()

	diff := DiffResult{Changes: []DiffChange{
		{Type: DiffChangeNewAction, Service: "stripe", Operation: "POST /v1/refunds", CandidateSideEffect: SideEffectFinancial},
		{Type: DiffChangeNewAction, Tool: "lookup_customer", CandidateSideEffect: SideEffectRead},
	}}

	got := ScoreRisk(diff, EvaluationResult{})
	if got.Level != RiskLevelHigh {
		t.Fatalf("Level = %q, want %q", got.Level, RiskLevelHigh)
	}
	if len(got.Reasons) != 1 {
		t.Fatalf("Reasons = %#v, want exactly one high-risk explanation", got.Reasons)
	}
}

func TestScoreRiskMediumOnNewWriteAction(t *testing.T) {
	t.Parallel()

	diff := DiffResult{Changes: []DiffChange{
		{Type: DiffChangeNewAction, Tool: "update_ticket", CandidateSideEffect: SideEffectWrite},
	}}

	got := ScoreRisk(diff, EvaluationResult{})
	if got.Level != RiskLevelMedium {
		t.Fatalf("Level = %q, want %q", got.Level, RiskLevelMedium)
	}
}

func TestScoreRiskLowOnReadOnlyChanges(t *testing.T) {
	t.Parallel()

	diff := DiffResult{
		Summary: DiffSummary{NewActions: 1},
		Changes: []DiffChange{
			{Type: DiffChangeNewAction, Tool: "lookup_customer", CandidateSideEffect: SideEffectRead},
			{Type: DiffChangeModels, Service: "openai", Operation: "chat.completions.create"},
		},
	}

	got := ScoreRisk(diff, EvaluationResult{})
	if got.Level != RiskLevelLow {
		t.Fatalf("Level = %q, want %q", got.Level, RiskLevelLow)
	}
}

func TestScoreRiskLowWhenNoChanges(t *testing.T) {
	t.Parallel()

	got := ScoreRisk(DiffResult{}, EvaluationResult{})
	if got.Level != RiskLevelLow {
		t.Fatalf("Level = %q, want %q", got.Level, RiskLevelLow)
	}
}
