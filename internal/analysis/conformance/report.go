package conformance

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Report struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Summary     ReportSummary `json:"summary"`
	Results     []Result      `json:"results"`
}

type ReportSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

func NewReport(generatedAt time.Time, results []Result) Report {
	report := Report{
		GeneratedAt: generatedAt.UTC(),
		Results:     append([]Result(nil), results...),
	}
	report.Summary.Total = len(results)
	for _, result := range results {
		switch result.Status {
		case ResultStatusPassed:
			report.Summary.Passed++
		case ResultStatusFailed:
			report.Summary.Failed++
		case ResultStatusSkipped:
			report.Summary.Skipped++
		case ResultStatusError:
			report.Summary.Errors++
		}
	}
	return report
}

func (r Report) HasDriftOrErrors() bool {
	return r.Summary.Failed > 0 || r.Summary.Errors > 0
}

func RenderJSON(report Report) ([]byte, error) {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func RenderMarkdown(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Stagehand Conformance\n\n")
	fmt.Fprintf(&b, "Generated: `%s`\n\n", report.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(
		&b,
		"Summary: **%d failed**, **%d errors**, %d passed, %d skipped, %d total\n\n",
		report.Summary.Failed,
		report.Summary.Errors,
		report.Summary.Passed,
		report.Summary.Skipped,
		report.Summary.Total,
	)
	if report.Summary.Total == 0 {
		b.WriteString("No conformance cases ran.\n")
		return b.String()
	}

	b.WriteString("| Case | Service | Status | Matched | Failing diffs | Tolerated diffs | Missing | Extra |\n")
	b.WriteString("| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, result := range report.Results {
		fmt.Fprintf(
			&b,
			"| `%s` | `%s` | `%s` | %d | %d | %d | %d | %d |\n",
			markdownCell(result.CaseID),
			markdownCell(result.Service),
			markdownCell(string(result.Status)),
			result.Summary.MatchedInteractions,
			result.Summary.FailingDiffs,
			result.Summary.ToleratedDiffs,
			result.Summary.MissingInteractions,
			result.Summary.ExtraInteractions,
		)
	}

	writeFailureSection(&b, "Drift and errors", report, map[ResultStatus]bool{
		ResultStatusFailed: true,
		ResultStatusError:  true,
	})
	writeFailureSection(&b, "Skipped cases", report, map[ResultStatus]bool{
		ResultStatusSkipped: true,
	})
	return b.String()
}

func writeFailureSection(b *strings.Builder, title string, report Report, statuses map[ResultStatus]bool) {
	wroteTitle := false
	for _, result := range report.Results {
		if !statuses[result.Status] {
			continue
		}
		if !wroteTitle {
			fmt.Fprintf(b, "\n### %s\n\n", title)
			wroteTitle = true
		}
		fmt.Fprintf(b, "- `%s` `%s`", result.Status, result.CaseID)
		if len(result.Failures) == 0 {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, ": %s", result.Failures[0].Message)
		if len(result.Failures) > 1 {
			fmt.Fprintf(b, " (+%d more)", len(result.Failures)-1)
		}
		b.WriteString("\n")
	}
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", " ")
	return value
}
