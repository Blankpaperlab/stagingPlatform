package recorder

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"stagehand/internal/version"
)

func TestRunValidateCompleteArtifact(t *testing.T) {
	run := validRun()

	if err := run.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !run.ReplayEligible() {
		t.Fatal("ReplayEligible() = false, want true for complete run")
	}
}

func TestRunValidateRequiresIntegrityIssuesForIncompleteAndCorrupted(t *testing.T) {
	incomplete := validRun()
	incomplete.Status = RunStatusIncomplete
	incomplete.EndedAt = nil
	incomplete.IntegrityIssues = nil

	err := incomplete.Validate()
	if err == nil || !strings.Contains(err.Error(), "integrity_issues must contain at least one issue") {
		t.Fatalf("Validate() missing incomplete integrity issue error, got %v", err)
	}

	corrupted := validRun()
	corrupted.Status = RunStatusCorrupted
	corrupted.IntegrityIssues = nil

	err = corrupted.Validate()
	if err == nil || !strings.Contains(err.Error(), "integrity_issues must contain at least one issue") {
		t.Fatalf("Validate() missing corrupted integrity issue error, got %v", err)
	}
}

func TestRunValidateRejectsCompleteRunWithIntegrityIssues(t *testing.T) {
	run := validRun()
	run.IntegrityIssues = []IntegrityIssue{
		{
			Code:    IntegrityIssueInterruptedWrite,
			Message: "unexpected write boundary interruption",
		},
	}

	err := run.Validate()
	if err == nil || !strings.Contains(err.Error(), "integrity_issues must be empty when status is") {
		t.Fatalf("Validate() expected complete run integrity issue failure, got %v", err)
	}
}

func TestRunValidateRejectsInteractionAndEventOrderingViolations(t *testing.T) {
	run := validRun()
	run.Interactions[0].Events[1].Sequence = 1
	run.Interactions[0].Events[1].TMS = -1
	run.Interactions[0].Events[1].SimTMS = -1
	run.Interactions = append(run.Interactions, run.Interactions[0])

	err := run.Validate()
	if err == nil {
		t.Fatal("Validate() expected ordering validation failure")
	}

	errText := err.Error()
	for _, want := range []string{
		"interactions[1].interaction_id",
		"interactions[1].sequence must be strictly increasing",
		"events[1].sequence must be strictly increasing",
		"events[1].t_ms must be non-decreasing",
		"events[1].sim_t_ms must be non-decreasing",
	} {
		if !strings.Contains(errText, want) {
			t.Fatalf("validation error %q missing from %v", want, err)
		}
	}
}

func TestRunValidateRejectsScrubPolicyMismatch(t *testing.T) {
	run := validRun()
	run.Interactions[0].ScrubReport.ScrubPolicyVersion = "v2"

	err := run.Validate()
	if err == nil || !strings.Contains(err.Error(), "scrub_report.scrub_policy_version must match run scrub_policy_version") {
		t.Fatalf("Validate() expected scrub policy mismatch, got %v", err)
	}
}

func TestRunJSONRoundTrip(t *testing.T) {
	run := validRun()

	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded Run
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if err := decoded.Validate(); err != nil {
		t.Fatalf("decoded.Validate() error = %v", err)
	}

	if decoded.RunID != run.RunID {
		t.Fatalf("decoded.RunID = %q, want %q", decoded.RunID, run.RunID)
	}
}

func validRun() Run {
	startedAt := time.Date(2026, time.April, 21, 10, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(402 * time.Millisecond)

	return Run{
		SchemaVersion:      ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     version.ArtifactVersion,
		ScrubPolicyVersion: "v1",
		RunID:              "run_abc123",
		SessionName:        "onboarding-flow",
		Mode:               RunModeRecord,
		Status:             RunStatusComplete,
		StartedAt:          startedAt,
		EndedAt:            &endedAt,
		GitSHA:             "abcdef123456",
		Interactions: []Interaction{
			{
				RunID:         "run_abc123",
				InteractionID: "int_47",
				Sequence:      47,
				Service:       "openai",
				Operation:     "chat.completions.create",
				Protocol:      ProtocolHTTPS,
				Streaming:     true,
				Request: Request{
					URL:    "https://api.openai.com/v1/chat/completions",
					Method: "POST",
					Headers: map[string][]string{
						"content-type": []string{"application/json"},
					},
					Body: map[string]any{
						"model": "gpt-5.4",
					},
				},
				Events: []Event{
					{
						Sequence: 1,
						TMS:      0,
						SimTMS:   0,
						Type:     EventTypeRequestSent,
					},
					{
						Sequence: 2,
						TMS:      234,
						SimTMS:   234,
						Type:     EventTypeStreamChunk,
						Data: map[string]any{
							"delta": "Hello",
						},
					},
					{
						Sequence:            3,
						TMS:                 341,
						SimTMS:              341,
						Type:                EventTypeToolCallStart,
						NestedInteractionID: "int_48",
					},
					{
						Sequence: 4,
						TMS:      402,
						SimTMS:   402,
						Type:     EventTypeStreamEnd,
						Data: map[string]any{
							"finish_reason": "tool_calls",
						},
					},
				},
				ExtractedEntities: []ExtractedEntity{
					{
						Type: "assistant_message",
						ID:   "msg_123",
						Attrs: map[string]any{
							"role": "assistant",
						},
					},
				},
				ScrubReport: ScrubReport{
					RedactedPaths: []string{
						"request.headers.authorization",
						"request.body.messages[0].content",
					},
					ScrubPolicyVersion: "v1",
					SessionSaltID:      "salt_xyz",
				},
				LatencyMS: 402,
			},
		},
	}
}
