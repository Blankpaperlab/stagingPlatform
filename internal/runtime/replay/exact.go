package replay

import (
	"fmt"

	"stagehand/internal/recorder"
)

type Result struct {
	Mode              string   `json:"mode"`
	RunID             string   `json:"run_id"`
	SessionName       string   `json:"session_name"`
	Status            string   `json:"status"`
	InteractionCount  int      `json:"interaction_count"`
	Services          []string `json:"services"`
	FallbackTiersUsed []string `json:"fallback_tiers_used"`
	ReplayEligible    bool     `json:"replay_eligible"`
}

func SummarizeExactSource(run recorder.Run) (Result, error) {
	if !run.ReplayEligible() {
		return Result{}, fmt.Errorf("run %q is not replay-eligible; status is %q", run.RunID, run.Status)
	}

	serviceSet := map[string]struct{}{}
	fallbackSet := map[string]struct{}{}
	services := make([]string, 0, len(run.Interactions))
	fallbacks := make([]string, 0, len(run.Interactions))
	for _, interaction := range run.Interactions {
		if _, ok := serviceSet[interaction.Service]; !ok {
			serviceSet[interaction.Service] = struct{}{}
			services = append(services, interaction.Service)
		}

		if interaction.FallbackTier == "" {
			continue
		}
		tier := string(interaction.FallbackTier)
		if _, ok := fallbackSet[tier]; ok {
			continue
		}
		fallbackSet[tier] = struct{}{}
		fallbacks = append(fallbacks, tier)
	}

	return Result{
		Mode:              "exact",
		RunID:             run.RunID,
		SessionName:       run.SessionName,
		Status:            string(run.Status),
		InteractionCount:  len(run.Interactions),
		Services:          services,
		FallbackTiersUsed: fallbacks,
		ReplayEligible:    true,
	}, nil
}
