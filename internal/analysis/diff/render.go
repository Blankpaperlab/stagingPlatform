package diff

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Summary struct {
	TotalChanges         int `json:"total_changes"`
	FailingChanges       int `json:"failing_changes"`
	InformationalChanges int `json:"informational_changes"`
}

type JSONReport struct {
	BaseRunID            string   `json:"base_run_id"`
	CandidateRunID       string   `json:"candidate_run_id"`
	SessionName          string   `json:"session_name"`
	Summary              Summary  `json:"summary"`
	FailingChanges       []Change `json:"failing_changes"`
	InformationalChanges []Change `json:"informational_changes"`
	Changes              []Change `json:"changes"`
}

func (r Result) Summary() Summary {
	summary := Summary{
		TotalChanges: len(r.Changes),
	}
	for _, change := range r.Changes {
		if change.Failing {
			summary.FailingChanges++
		} else {
			summary.InformationalChanges++
		}
	}
	return summary
}

func (r Result) FailingChanges() []Change {
	return filterChanges(r.Changes, true)
}

func (r Result) InformationalChanges() []Change {
	return filterChanges(r.Changes, false)
}

func RenderJSON(result Result) ([]byte, error) {
	report := JSONReport{
		BaseRunID:            result.BaseRunID,
		CandidateRunID:       result.CandidateRunID,
		SessionName:          result.SessionName,
		Summary:              result.Summary(),
		FailingChanges:       result.FailingChanges(),
		InformationalChanges: result.InformationalChanges(),
		Changes:              append([]Change(nil), result.Changes...),
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func RenderTerminal(result Result) string {
	var b strings.Builder
	summary := result.Summary()

	fmt.Fprintf(&b, "Stagehand diff: %s -> %s\n", empty(result.BaseRunID, "base"), empty(result.CandidateRunID, "candidate"))
	fmt.Fprintf(&b, "Session: %s\n", result.SessionName)
	fmt.Fprintf(&b, "Changes: %d failing, %d informational, %d total\n", summary.FailingChanges, summary.InformationalChanges, summary.TotalChanges)

	if summary.TotalChanges == 0 {
		b.WriteString("\nNo interaction changes detected.\n")
		return b.String()
	}

	writeTerminalSection(&b, "Failing changes", result.FailingChanges())
	writeTerminalSection(&b, "Informational changes", result.InformationalChanges())
	return b.String()
}

func RenderGitHubMarkdown(result Result) string {
	var b strings.Builder
	summary := result.Summary()

	b.WriteString("### Stagehand Diff\n\n")
	fmt.Fprintf(&b, "- Base run: `%s`\n", markdownInline(empty(result.BaseRunID, "base")))
	fmt.Fprintf(&b, "- Candidate run: `%s`\n", markdownInline(empty(result.CandidateRunID, "candidate")))
	fmt.Fprintf(&b, "- Session: `%s`\n", markdownInline(result.SessionName))
	fmt.Fprintf(&b, "- Summary: **%d failing**, %d informational, %d total\n", summary.FailingChanges, summary.InformationalChanges, summary.TotalChanges)

	if summary.TotalChanges == 0 {
		b.WriteString("\nNo interaction changes detected.\n")
		return b.String()
	}

	writeMarkdownSection(&b, "Failing Changes", result.FailingChanges())
	writeMarkdownSection(&b, "Informational Changes", result.InformationalChanges())
	return b.String()
}

func filterChanges(changes []Change, failing bool) []Change {
	filtered := make([]Change, 0)
	for _, change := range changes {
		if change.Failing == failing {
			filtered = append(filtered, change)
		}
	}
	return filtered
}

func writeTerminalSection(b *strings.Builder, title string, changes []Change) {
	if len(changes) == 0 {
		return
	}

	fmt.Fprintf(b, "\n%s:\n", title)
	for _, change := range changes {
		fmt.Fprintf(
			b,
			"- %s %s %s base_seq=%s candidate_seq=%s base_id=%s candidate_id=%s",
			change.Type,
			empty(change.Service, "-"),
			empty(change.Operation, "-"),
			sequence(change.BaseSequence),
			sequence(change.CandidateSequence),
			empty(change.BaseInteractionID, "-"),
			empty(change.CandidateInteractionID, "-"),
		)
		if change.BaseFallbackTier != "" || change.CandidateFallbackTier != "" {
			fmt.Fprintf(b, " fallback=%s->%s", empty(string(change.BaseFallbackTier), "none"), empty(string(change.CandidateFallbackTier), "none"))
		}
		if change.Details != "" {
			fmt.Fprintf(b, " - %s", change.Details)
		}
		b.WriteString("\n")
	}
}

func writeMarkdownSection(b *strings.Builder, title string, changes []Change) {
	if len(changes) == 0 {
		return
	}

	fmt.Fprintf(b, "\n#### %s\n\n", title)
	b.WriteString("| Type | Service | Operation | Base | Candidate | Details |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, change := range changes {
		fmt.Fprintf(
			b,
			"| `%s` | %s | %s | %s | %s | %s |\n",
			markdownInline(string(change.Type)),
			markdownCell(change.Service),
			markdownCell(change.Operation),
			markdownCell(interactionRef(change.BaseInteractionID, change.BaseSequence)),
			markdownCell(interactionRef(change.CandidateInteractionID, change.CandidateSequence)),
			markdownCell(changeDetails(change)),
		)
	}
}

func interactionRef(id string, sequenceValue int) string {
	if id == "" && sequenceValue == 0 {
		return "-"
	}
	if sequenceValue == 0 {
		return id
	}
	if id == "" {
		return "seq " + sequence(sequenceValue)
	}
	return fmt.Sprintf("%s (seq %d)", id, sequenceValue)
}

func changeDetails(change Change) string {
	details := change.Details
	if change.BaseFallbackTier != "" || change.CandidateFallbackTier != "" {
		fallback := fmt.Sprintf("fallback %s -> %s", empty(string(change.BaseFallbackTier), "none"), empty(string(change.CandidateFallbackTier), "none"))
		if details == "" {
			return fallback
		}
		return details + "; " + fallback
	}
	return details
}

func sequence(value int) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", value)
}

func empty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return empty(value, "-")
}

func markdownInline(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}
