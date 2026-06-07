package contracts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"stagehand/internal/recorder"
)

func TestDiffRunsDetectsContractBehaviorChanges(t *testing.T) {
	t.Parallel()

	base := recorder.Run{
		RunID:       "run_base",
		SessionName: "contract-diff-flow",
		StartedAt:   time.Now().UTC(),
		Interactions: []recorder.Interaction{
			contractInteraction("run_base", 1, "openai", "chat.completions.create", recorder.ProtocolHTTPS, "POST", "https://api.openai.com/v1/chat/completions", map[string]any{
				"model": "gpt-5.4",
				"messages": []map[string]string{
					{"role": "user", "content": "refund order 123"},
				},
			}),
			contractInteraction("run_base", 2, "stagehand.tool", "lookup_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/lookup_customer", map[string]any{
				"name":        "lookup_customer",
				"side_effect": "read",
			}),
			contractInteraction("run_base", 3, "stripe", "GET /v1/customers/search", recorder.ProtocolHTTPS, "GET", "https://api.stripe.com/v1/customers/search", nil),
		},
	}
	candidate := recorder.Run{
		RunID:       "run_candidate",
		SessionName: "contract-diff-flow",
		StartedAt:   time.Now().UTC(),
		Interactions: []recorder.Interaction{
			contractInteraction("run_candidate", 1, "openai", "chat.completions.create", recorder.ProtocolHTTPS, "POST", "https://api.openai.com/v1/chat/completions", map[string]any{
				"model": "gpt-5.5",
				"messages": []map[string]string{
					{"role": "user", "content": "refund order 456"},
				},
			}),
			contractInteraction("run_candidate", 2, "stagehand.tool", "lookup_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/lookup_customer", map[string]any{
				"name":        "lookup_customer",
				"side_effect": "write",
			}),
			contractInteraction("run_candidate", 3, "slack", "POST /chat.postMessage", recorder.ProtocolHTTPS, "POST", "https://slack.example/chat.postMessage", nil),
		},
	}
	candidate.Interactions[0].FallbackTier = recorder.FallbackTierNearestNeighbor

	result, err := DiffRuns(base, candidate)
	if err != nil {
		t.Fatalf("DiffRuns() error = %v", err)
	}
	if result.Summary.TotalChanges != 6 {
		t.Fatalf("Summary.TotalChanges = %d, want 6: %#v", result.Summary.TotalChanges, result.Changes)
	}
	if result.Summary.NewActions != 1 || result.Summary.RemovedActions != 1 || result.Summary.SideEffectChanges != 1 || result.Summary.FallbackTierChanges != 1 || result.Summary.ModelChanges != 1 || result.Summary.PromptChanges != 1 {
		t.Fatalf("Summary = %#v, want one of each AB5 change category", result.Summary)
	}
	modelChange := findDiffChange(t, result, DiffChangeModels)
	if modelChange.BaseModels[0] != "gpt-5.4" || modelChange.CandidateModels[0] != "gpt-5.5" {
		t.Fatalf("model change = %#v, want gpt-5.4 -> gpt-5.5", modelChange)
	}
	promptChange := findDiffChange(t, result, DiffChangePrompts)
	if len(promptChange.BasePrompts) != 1 || len(promptChange.CandidatePrompts) != 1 || promptChange.BasePrompts[0].Hash == promptChange.CandidatePrompts[0].Hash {
		t.Fatalf("prompt change = %#v, want changed prompt digests", promptChange)
	}
}

func TestDiffRenderers(t *testing.T) {
	t.Parallel()

	result := DiffResult{
		BaseRunID:      "run_base",
		CandidateRunID: "run_candidate",
		SessionName:    "contract-diff-flow",
		Changes: []DiffChange{
			{
				Type:      DiffChangeNewAction,
				Service:   "slack",
				Operation: "POST /chat.postMessage",
				Details:   "action appears only in candidate run",
				CandidateEvidence: []InteractionEvidence{
					{InteractionID: "int_slack", Sequence: 3},
				},
			},
		},
	}
	result.Summary = summarizeDiffChanges(result.Changes)

	encoded, err := RenderDiffJSON(result)
	if err != nil {
		t.Fatalf("RenderDiffJSON() error = %v", err)
	}
	var decoded DiffResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(RenderDiffJSON()) error = %v\npayload=%s", err, string(encoded))
	}
	if decoded.Summary.NewActions != 1 {
		t.Fatalf("decoded.Summary = %#v, want one new action", decoded.Summary)
	}

	terminal := RenderDiffTerminal(result)
	for _, want := range []string{"Stagehand contract diff", "new_action", "slack POST /chat.postMessage"} {
		if !strings.Contains(terminal, want) {
			t.Fatalf("RenderDiffTerminal() missing %q\n%s", want, terminal)
		}
	}
	markdown := RenderDiffGitHubMarkdown(result)
	for _, want := range []string{"### Stagehand Contract Diff", "| Type | Action |", "`new_action`"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("RenderDiffGitHubMarkdown() missing %q\n%s", want, markdown)
		}
	}
}

func TestDiffRenderersHandleEmptyDiff(t *testing.T) {
	t.Parallel()

	result := DiffResult{BaseRunID: "run_base", CandidateRunID: "run_candidate", SessionName: "empty-flow"}
	if !strings.Contains(RenderDiffTerminal(result), "No contract behavior changes detected.") {
		t.Fatalf("RenderDiffTerminal(empty) missing no-change message")
	}
	if !strings.Contains(RenderDiffGitHubMarkdown(result), "No contract behavior changes detected.") {
		t.Fatalf("RenderDiffGitHubMarkdown(empty) missing no-change message")
	}
}

func findDiffChange(t *testing.T, result DiffResult, changeType DiffChangeType) DiffChange {
	t.Helper()
	for _, change := range result.Changes {
		if change.Type == changeType {
			return change
		}
	}
	t.Fatalf("change %q not found in %#v", changeType, result.Changes)
	return DiffChange{}
}
