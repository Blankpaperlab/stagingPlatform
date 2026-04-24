package fallback

import (
	"context"
	"errors"
	"testing"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
)

func TestMatcherUsesExactMatchBeforeFallbacks(t *testing.T) {
	t.Parallel()

	recorded := []recorder.Interaction{
		fallbackInteraction("int_001", 1, "https://api.example.test/v1/customers?expand=invoice&limit=10", map[string]any{
			"customer_id": "cus_001",
		}),
	}
	matcher, err := NewMatcher(recorded)
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}

	request := fallbackInteraction("request_001", 1, "https://api.example.test/v1/customers?limit=10&expand=invoice", map[string]any{
		"customer_id": "cus_001",
	})
	result, err := matcher.Match(context.Background(), request)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	if result.Tier != recorder.FallbackTierExact {
		t.Fatalf("Tier = %q, want %q", result.Tier, recorder.FallbackTierExact)
	}
	if result.Interaction.InteractionID != "int_001" {
		t.Fatalf("InteractionID = %q, want int_001", result.Interaction.InteractionID)
	}
	if result.Interaction.FallbackTier != recorder.FallbackTierExact {
		t.Fatalf("Interaction.FallbackTier = %q, want exact", result.Interaction.FallbackTier)
	}
}

func TestMatcherUsesNearestNeighborForRequestVariation(t *testing.T) {
	t.Parallel()

	recorded := []recorder.Interaction{
		fallbackInteraction("int_low_score", 1, "https://api.example.test/v1/customers?expand=balance", map[string]any{
			"customer_id": "cus_other",
			"currency":    "eur",
			"metadata":    map[string]any{"flow": "different"},
		}),
		fallbackInteraction("int_nearest", 2, "https://api.example.test/v1/customers?expand=invoice", map[string]any{
			"customer_id": "cus_001",
			"currency":    "usd",
			"amount":      1000,
		}),
	}
	matcher, err := NewMatcher(recorded)
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}

	request := fallbackInteraction("request_001", 1, "https://api.example.test/v1/customers?expand=invoice", map[string]any{
		"customer_id": "cus_001",
		"currency":    "usd",
		"amount":      1200,
	})
	result, err := matcher.Match(context.Background(), request)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	if result.Tier != recorder.FallbackTierNearestNeighbor {
		t.Fatalf("Tier = %q, want %q", result.Tier, recorder.FallbackTierNearestNeighbor)
	}
	if result.Interaction.InteractionID != "int_nearest" {
		t.Fatalf("InteractionID = %q, want int_nearest", result.Interaction.InteractionID)
	}
	if result.Score < minimumNearestScore {
		t.Fatalf("Score = %f, want >= %f", result.Score, minimumNearestScore)
	}
	if result.Interaction.FallbackTier != recorder.FallbackTierNearestNeighbor {
		t.Fatalf("Interaction.FallbackTier = %q, want nearest_neighbor", result.Interaction.FallbackTier)
	}
}

func TestMatcherHonorsAllowedFallbackTiersFromConfig(t *testing.T) {
	t.Parallel()

	cfg := config.DefaultConfig()
	cfg.Fallback.AllowedTiers = []config.FallbackTier{config.FallbackTierExact}

	recorded := []recorder.Interaction{
		fallbackInteraction("int_001", 1, "https://api.example.test/v1/customers", map[string]any{
			"customer_id": "cus_001",
		}),
	}
	matcher, err := NewMatcher(recorded, WithConfig(cfg))
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}

	request := fallbackInteraction("request_001", 1, "https://api.example.test/v1/customers", map[string]any{
		"customer_id": "cus_002",
	})
	_, err = matcher.Match(context.Background(), request)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("Match() error = %v, want ErrNoMatch when nearest is disabled", err)
	}
}

func TestMatcherUsesStateSynthesisHookAfterMatchMiss(t *testing.T) {
	t.Parallel()

	request := fallbackInteraction("request_001", 1, "https://api.example.test/v1/customers", map[string]any{
		"customer_id": "cus_missing",
	})

	called := false
	matcher, err := NewMatcher(
		nil,
		WithStateSynthesizer(SynthesizeFunc(func(ctx context.Context, input SynthesisInput) (recorder.Interaction, error) {
			called = true
			if input.Request.InteractionID != request.InteractionID {
				t.Fatalf("synthesis request = %q, want %q", input.Request.InteractionID, request.InteractionID)
			}
			if len(input.Candidates) != 0 {
				t.Fatalf("len(synthesis candidates) = %d, want 0", len(input.Candidates))
			}
			return fallbackInteraction("synthesized_001", 1, request.Request.URL, map[string]any{
				"customer_id": "cus_synthesized",
			}), nil
		})),
	)
	if err != nil {
		t.Fatalf("NewMatcher() error = %v", err)
	}

	result, err := matcher.Match(context.Background(), request)
	if err != nil {
		t.Fatalf("Match() error = %v", err)
	}

	if !called {
		t.Fatal("state synthesis hook was not called")
	}
	if result.Tier != recorder.FallbackTierStateSynthesis {
		t.Fatalf("Tier = %q, want %q", result.Tier, recorder.FallbackTierStateSynthesis)
	}
	if result.Interaction.InteractionID != "synthesized_001" {
		t.Fatalf("InteractionID = %q, want synthesized_001", result.Interaction.InteractionID)
	}
	if result.Interaction.FallbackTier != recorder.FallbackTierStateSynthesis {
		t.Fatalf("Interaction.FallbackTier = %q, want state_synthesis", result.Interaction.FallbackTier)
	}
}

func TestMatcherRejectsUnimplementedLLMSynthesisTier(t *testing.T) {
	t.Parallel()

	_, err := NewMatcher(nil, WithAllowedTiers([]recorder.FallbackTier{
		recorder.FallbackTierExact,
		recorder.FallbackTierLLMSynthesis,
	}))
	if err == nil {
		t.Fatal("NewMatcher() expected llm_synthesis rejection")
	}
}

func fallbackInteraction(id string, sequence int, rawURL string, body map[string]any) recorder.Interaction {
	return recorder.Interaction{
		RunID:         "run_fallback_source",
		InteractionID: id,
		Sequence:      sequence,
		Service:       "example",
		Operation:     "POST /v1/customers",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    rawURL,
			Method: "POST",
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: body,
		},
		Events: []recorder.Event{
			{
				Sequence: 1,
				Type:     recorder.EventTypeRequestSent,
			},
			{
				Sequence: 2,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status_code": 200,
					"body":        map[string]any{"ok": true},
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fallback_test",
		},
	}
}
