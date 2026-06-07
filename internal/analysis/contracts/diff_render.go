package contracts

import (
	"encoding/json"
	"fmt"
	"strings"
)

func RenderDiffJSON(result DiffResult) ([]byte, error) {
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func RenderDiffTerminal(result DiffResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Stagehand contract diff: %s -> %s\n", emptyString(result.BaseRunID, "base"), emptyString(result.CandidateRunID, "candidate"))
	fmt.Fprintf(&b, "Session: %s\n", result.SessionName)
	fmt.Fprintf(&b, "Changes: %d total (new %d, removed %d, side-effect %d, fallback %d, model %d, prompt %d)\n",
		result.Summary.TotalChanges,
		result.Summary.NewActions,
		result.Summary.RemovedActions,
		result.Summary.SideEffectChanges,
		result.Summary.FallbackTierChanges,
		result.Summary.ModelChanges,
		result.Summary.PromptChanges,
	)
	if result.Summary.TotalChanges == 0 {
		b.WriteString("\nNo contract behavior changes detected.\n")
		return b.String()
	}
	b.WriteString("\nChanges:\n")
	for _, change := range result.Changes {
		fmt.Fprintf(&b, "- %s %s", change.Type, diffActionLabel(change))
		if change.Details != "" {
			fmt.Fprintf(&b, ": %s", change.Details)
		}
		if base := firstEvidence(change.BaseEvidence); base != "" {
			fmt.Fprintf(&b, " base=%s", base)
		}
		if candidate := firstEvidence(change.CandidateEvidence); candidate != "" {
			fmt.Fprintf(&b, " candidate=%s", candidate)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func RenderDiffGitHubMarkdown(result DiffResult) string {
	var b strings.Builder
	b.WriteString("### Stagehand Contract Diff\n\n")
	fmt.Fprintf(&b, "- Base run: `%s`\n", markdownInline(emptyString(result.BaseRunID, "base")))
	fmt.Fprintf(&b, "- Candidate run: `%s`\n", markdownInline(emptyString(result.CandidateRunID, "candidate")))
	fmt.Fprintf(&b, "- Session: `%s`\n", markdownInline(result.SessionName))
	fmt.Fprintf(&b, "- Summary: **%d total**; %d new, %d removed, %d side-effect, %d fallback, %d model, %d prompt\n",
		result.Summary.TotalChanges,
		result.Summary.NewActions,
		result.Summary.RemovedActions,
		result.Summary.SideEffectChanges,
		result.Summary.FallbackTierChanges,
		result.Summary.ModelChanges,
		result.Summary.PromptChanges,
	)
	if result.Summary.TotalChanges == 0 {
		b.WriteString("\nNo contract behavior changes detected.\n")
		return b.String()
	}
	b.WriteString("\n| Type | Action | Base Evidence | Candidate Evidence | Details |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, change := range result.Changes {
		fmt.Fprintf(
			&b,
			"| `%s` | %s | %s | %s | %s |\n",
			markdownInline(string(change.Type)),
			markdownCell(diffActionLabel(change)),
			markdownCell(firstEvidence(change.BaseEvidence)),
			markdownCell(firstEvidence(change.CandidateEvidence)),
			markdownCell(change.Details),
		)
	}
	return b.String()
}

func diffActionLabel(change DiffChange) string {
	if strings.TrimSpace(change.Tool) != "" {
		return "tool:" + change.Tool
	}
	return strings.TrimSpace(change.Service + " " + change.Operation)
}

func firstEvidence(evidence []InteractionEvidence) string {
	if len(evidence) == 0 {
		return ""
	}
	item := evidence[0]
	if item.InteractionID == "" && item.Sequence == 0 {
		return ""
	}
	if item.InteractionID == "" {
		return fmt.Sprintf("seq %d", item.Sequence)
	}
	if item.Sequence == 0 {
		return item.InteractionID
	}
	return fmt.Sprintf("%s (seq %d)", item.InteractionID, item.Sequence)
}

func emptyString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return emptyString(value, "-")
}

func markdownInline(value string) string {
	return strings.ReplaceAll(value, "`", "'")
}
