package replay

import (
	"testing"

	"stagehand/internal/recorder"
)

func TestSummarizeExactSourceSummarizesReplayEligibleRun(t *testing.T) {
	run := recorder.Run{
		RunID:       "run_123",
		SessionName: "onboarding-flow",
		Status:      recorder.RunStatusComplete,
		Interactions: []recorder.Interaction{
			{Service: "openai", FallbackTier: recorder.FallbackTierExact},
			{Service: "stripe", FallbackTier: recorder.FallbackTierNearestNeighbor},
			{Service: "openai", FallbackTier: recorder.FallbackTierExact},
		},
	}

	result, err := SummarizeExactSource(run)
	if err != nil {
		t.Fatalf("SummarizeExactSource() error = %v", err)
	}

	if result.Mode != "exact" {
		t.Fatalf("result.Mode = %q, want exact", result.Mode)
	}
	if result.RunID != run.RunID {
		t.Fatalf("result.RunID = %q, want %q", result.RunID, run.RunID)
	}
	if result.SessionName != run.SessionName {
		t.Fatalf("result.SessionName = %q, want %q", result.SessionName, run.SessionName)
	}
	if result.Status != string(run.Status) {
		t.Fatalf("result.Status = %q, want %q", result.Status, run.Status)
	}
	if result.InteractionCount != len(run.Interactions) {
		t.Fatalf("result.InteractionCount = %d, want %d", result.InteractionCount, len(run.Interactions))
	}
	if !result.ReplayEligible {
		t.Fatal("result.ReplayEligible = false, want true")
	}

	wantServices := []string{"openai", "stripe"}
	if len(result.Services) != len(wantServices) {
		t.Fatalf("len(result.Services) = %d, want %d", len(result.Services), len(wantServices))
	}
	for idx, want := range wantServices {
		if result.Services[idx] != want {
			t.Fatalf("result.Services[%d] = %q, want %q", idx, result.Services[idx], want)
		}
	}

	wantTiers := []string{string(recorder.FallbackTierExact), string(recorder.FallbackTierNearestNeighbor)}
	if len(result.FallbackTiersUsed) != len(wantTiers) {
		t.Fatalf("len(result.FallbackTiersUsed) = %d, want %d", len(result.FallbackTiersUsed), len(wantTiers))
	}
	for idx, want := range wantTiers {
		if result.FallbackTiersUsed[idx] != want {
			t.Fatalf("result.FallbackTiersUsed[%d] = %q, want %q", idx, result.FallbackTiersUsed[idx], want)
		}
	}
}

func TestSummarizeExactSourceRejectsNonReplayEligibleRun(t *testing.T) {
	run := recorder.Run{
		RunID:       "run_456",
		SessionName: "refund-flow",
		Status:      recorder.RunStatusCorrupted,
	}

	_, err := SummarizeExactSource(run)
	if err == nil {
		t.Fatal("SummarizeExactSource() error = nil, want replay eligibility error")
	}
}
