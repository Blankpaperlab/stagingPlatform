package conformance

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewReportSummarizesStatuses(t *testing.T) {
	report := NewReport(time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC), []Result{
		{CaseID: "passed", Status: ResultStatusPassed},
		{CaseID: "failed", Status: ResultStatusFailed},
		{CaseID: "skipped", Status: ResultStatusSkipped},
		{CaseID: "error", Status: ResultStatusError},
	})

	if report.Summary.Total != 4 || report.Summary.Passed != 1 || report.Summary.Failed != 1 || report.Summary.Skipped != 1 || report.Summary.Errors != 1 {
		t.Fatalf("Summary = %#v, want one of each status", report.Summary)
	}
	if !report.HasDriftOrErrors() {
		t.Fatal("HasDriftOrErrors() = false, want true")
	}
}

func TestRenderJSON(t *testing.T) {
	report := NewReport(time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC), []Result{
		{CaseID: "case-1", Service: "stripe", Status: ResultStatusPassed},
	})
	encoded, err := RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v", err)
	}
	var decoded Report
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\n%s", err, string(encoded))
	}
	if decoded.Summary.Passed != 1 {
		t.Fatalf("decoded Summary = %#v, want passed case", decoded.Summary)
	}
}

func TestRenderMarkdownHighlightsFailuresAndSkips(t *testing.T) {
	report := NewReport(time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC), []Result{
		{CaseID: "failed", Service: "stripe", Status: ResultStatusFailed, Failures: []Failure{{Message: "field mismatch"}}},
		{CaseID: "skipped", Service: "openai", Status: ResultStatusSkipped, Failures: []Failure{{Message: "missing credentials"}}},
	})
	markdown := RenderMarkdown(report)
	for _, expected := range []string{
		"## Stagehand Conformance",
		"Summary: **1 failed**, **0 errors**, 0 passed, 1 skipped, 2 total",
		"### Drift and errors",
		"field mismatch",
		"### Skipped cases",
		"missing credentials",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("markdown missing %q\n%s", expected, markdown)
		}
	}
}
