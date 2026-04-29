package diff

import (
	"encoding/json"
	"strings"
	"testing"

	"stagehand/internal/recorder"
)

func TestResultPartitionsFailingAndInformationalChanges(t *testing.T) {
	t.Parallel()

	result := renderFixtureResult()
	summary := result.Summary()

	if summary.TotalChanges != 2 || summary.FailingChanges != 1 || summary.InformationalChanges != 1 {
		t.Fatalf("Summary() = %#v, want 2 total, 1 failing, 1 informational", summary)
	}
	if len(result.FailingChanges()) != 1 || result.FailingChanges()[0].Type != ChangeFallbackRegression {
		t.Fatalf("FailingChanges() = %#v, want fallback regression", result.FailingChanges())
	}
	if len(result.InformationalChanges()) != 1 || result.InformationalChanges()[0].Type != ChangeOrderingChanged {
		t.Fatalf("InformationalChanges() = %#v, want ordering change", result.InformationalChanges())
	}
}

func TestRenderJSONEmitsMachineReadableDiff(t *testing.T) {
	t.Parallel()

	encoded, err := RenderJSON(renderFixtureResult())
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}

	var report JSONReport
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatalf("json.Unmarshal(RenderJSON()) error = %v\npayload=%s", err, string(encoded))
	}

	if report.BaseRunID != "run_base" || report.CandidateRunID != "run_candidate" {
		t.Fatalf("report run IDs = %q -> %q, want run_base -> run_candidate", report.BaseRunID, report.CandidateRunID)
	}
	if report.Summary.FailingChanges != 1 || len(report.FailingChanges) != 1 {
		t.Fatalf("report failing changes = %#v, summary = %#v", report.FailingChanges, report.Summary)
	}
	if report.FailingChanges[0].Failing != true {
		t.Fatalf("report.FailingChanges[0].Failing = false, want true")
	}
}

func TestRenderTerminalEmitsSummaryAndSections(t *testing.T) {
	t.Parallel()

	rendered := RenderTerminal(renderFixtureResult())

	for _, want := range []string{
		"Stagehand diff: run_base -> run_candidate",
		"Changes: 1 failing, 1 informational, 2 total",
		"Failing changes:",
		"fallback_regression",
		"Informational changes:",
		"ordering_changed",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderTerminal() missing %q\noutput=%s", want, rendered)
		}
	}
}

func TestRenderGitHubMarkdownEmitsCommentSummary(t *testing.T) {
	t.Parallel()

	rendered := RenderGitHubMarkdown(renderFixtureResult())

	for _, want := range []string{
		"### Stagehand Diff",
		"**1 failing**, 1 informational, 2 total",
		"#### Failing Changes",
		"| Type | Service | Operation | Base | Candidate | Details |",
		"`fallback_regression`",
		"#### Informational Changes",
		"`ordering_changed`",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderGitHubMarkdown() missing %q\noutput=%s", want, rendered)
		}
	}
}

func TestRenderersHandleEmptyDiff(t *testing.T) {
	t.Parallel()

	result := Result{
		BaseRunID:      "run_base",
		CandidateRunID: "run_candidate",
		SessionName:    "session-a",
	}

	if !strings.Contains(RenderTerminal(result), "No interaction changes detected.") {
		t.Fatalf("RenderTerminal(empty) missing no-change message")
	}
	if !strings.Contains(RenderGitHubMarkdown(result), "No interaction changes detected.") {
		t.Fatalf("RenderGitHubMarkdown(empty) missing no-change message")
	}
}

func renderFixtureResult() Result {
	return Result{
		BaseRunID:      "run_base",
		CandidateRunID: "run_candidate",
		SessionName:    "session-a",
		Changes: []Change{
			{
				Type:                   ChangeFallbackRegression,
				Failing:                true,
				BaseInteractionID:      "base_int",
				CandidateInteractionID: "candidate_int",
				BaseSequence:           1,
				CandidateSequence:      1,
				Service:                "openai",
				Operation:              "chat.completions.create",
				BaseFallbackTier:       recorder.FallbackTierExact,
				CandidateFallbackTier:  recorder.FallbackTierNearestNeighbor,
				Details:                "candidate used a less exact fallback tier",
			},
			{
				Type:                   ChangeOrderingChanged,
				Failing:                false,
				BaseInteractionID:      "base_second",
				CandidateInteractionID: "candidate_second",
				BaseSequence:           2,
				CandidateSequence:      1,
				Service:                "stripe",
				Operation:              "customers.create",
				Details:                "matched interaction sequence changed",
			},
		},
	}
}
