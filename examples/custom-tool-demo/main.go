package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	analysisassertions "stagehand/internal/analysis/assertions"
	analysisdiff "stagehand/internal/analysis/diff"
	"stagehand/internal/recorder"
	"stagehand/internal/runtime/injection"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
	"stagehand/internal/version"
)

const demoSessionName = "custom-tool-demo"
const demoStoragePath = ".stagehand/runs"

type DemoSummary struct {
	SessionName string             `json:"session_name"`
	Record      WorkflowSummary    `json:"record"`
	Replay      WorkflowSummary    `json:"replay"`
	Regression  RegressionSummary  `json:"regression"`
	Assertion   AssertionSummary   `json:"assertion"`
	Injection   InjectionSummary   `json:"injection"`
	Artifacts   *ArtifactSummary   `json:"artifacts,omitempty"`
	Commands    map[string]string  `json:"commands,omitempty"`
	Services    []InteractionLabel `json:"services"`
}

type WorkflowSummary struct {
	RunID            string `json:"run_id"`
	Mode             string `json:"mode"`
	InteractionCount int    `json:"interaction_count"`
	OpenAI           bool   `json:"openai"`
	LookupTool       bool   `json:"lookup_customer_tool"`
	BillingAPI       bool   `json:"internal_billing_api"`
	Stripe           bool   `json:"stripe"`
	SupportAPI       bool   `json:"support_ticket_api"`
}

type RegressionSummary struct {
	BaseRunID       string `json:"base_run_id"`
	CandidateRunID  string `json:"candidate_run_id"`
	TotalChanges    int    `json:"total_changes"`
	FailingChanges  int    `json:"failing_changes"`
	FirstType       string `json:"first_type,omitempty"`
	FirstService    string `json:"first_service,omitempty"`
	FirstOperation  string `json:"first_operation,omitempty"`
	ChangedBehavior string `json:"changed_behavior,omitempty"`
}

type AssertionSummary struct {
	RunID     string `json:"run_id"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	BeforeID  string `json:"before_interaction_id,omitempty"`
	AfterID   string `json:"after_interaction_id,omitempty"`
	Violation string `json:"violation"`
}

type InjectionSummary struct {
	RunID        string   `json:"run_id"`
	Tool         string   `json:"tool"`
	ErrorType    string   `json:"error_type"`
	ErrorClass   string   `json:"error_class"`
	Message      string   `json:"message"`
	Provenance   []string `json:"provenance"`
	RecoveredVia string   `json:"recovered_via"`
}

type ArtifactSummary struct {
	Persisted         bool   `json:"persisted"`
	StoragePath       string `json:"storage_path"`
	DatabasePath      string `json:"database_path"`
	RecordRunID       string `json:"record_run_id"`
	ReplayRunID       string `json:"replay_run_id"`
	CandidateRunID    string `json:"candidate_run_id"`
	UnsafeRunID       string `json:"unsafe_run_id"`
	InjectedRunID     string `json:"injected_run_id"`
	InspectCommand    string `json:"inspect_command"`
	DiffCommand       string `json:"diff_command"`
	AssertCommand     string `json:"assert_command"`
	SessionInspectCmd string `json:"session_inspect_command"`
}

type InteractionLabel struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
	Protocol  string `json:"protocol"`
}

func main() {
	summary, err := RunCustomToolDemo(context.Background())
	if err != nil {
		fatal(err)
	}
	artifacts, err := PersistCustomToolDemoRuns(context.Background(), demoStoragePath)
	if err != nil {
		fatal(err)
	}
	summary.Artifacts = &artifacts
	summary.Commands = map[string]string{
		"inspect": artifacts.SessionInspectCmd,
		"diff":    artifacts.DiffCommand,
		"assert":  artifacts.AssertCommand,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		fatal(err)
	}
}

func RunCustomToolDemo(ctx context.Context) (DemoSummary, error) {
	select {
	case <-ctx.Done():
		return DemoSummary{}, ctx.Err()
	default:
	}

	record := recordRun()
	replay := replayRun()
	candidate := candidateRun()
	regression, err := compareDemoRegression(record, candidate)
	if err != nil {
		return DemoSummary{}, err
	}
	assertion, err := evaluateUnsafeOrdering(unsafeOrderingRun())
	if err != nil {
		return DemoSummary{}, err
	}
	injected := injectionSummary(injectedToolRun())

	return DemoSummary{
		SessionName: demoSessionName,
		Record:      workflowSummary(record),
		Replay:      workflowSummary(replay),
		Regression:  regression,
		Assertion:   assertion,
		Injection:   injected,
		Services:    labels(record.Interactions),
	}, nil
}

func PersistCustomToolDemoRuns(ctx context.Context, storagePath string) (ArtifactSummary, error) {
	storagePath = strings.TrimSpace(storagePath)
	if storagePath == "" {
		storagePath = demoStoragePath
	}
	dbPath := sqliteDatabasePath(storagePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return ArtifactSummary{}, fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	if err := sqliteStore.DeleteSession(ctx, demoSessionName); err != nil && !errors.Is(err, store.ErrNotFound) {
		return ArtifactSummary{}, fmt.Errorf("clear existing demo session %q: %w", demoSessionName, err)
	}

	runs := []recorder.Run{recordRun(), replayRun(), candidateRun(), unsafeOrderingRun(), injectedToolRun()}
	for _, run := range runs {
		if err := persistDemoRun(ctx, sqliteStore, run); err != nil {
			return ArtifactSummary{}, err
		}
	}

	assertPath := filepath.Join("examples", "custom-tool-demo", "assertions.yml")
	return ArtifactSummary{
		Persisted:         true,
		StoragePath:       storagePath,
		DatabasePath:      dbPath,
		RecordRunID:       runs[0].RunID,
		ReplayRunID:       runs[1].RunID,
		CandidateRunID:    runs[2].RunID,
		UnsafeRunID:       runs[3].RunID,
		InjectedRunID:     runs[4].RunID,
		InspectCommand:    "go run ./cmd/stagehand inspect --run-id " + runs[0].RunID + " --show-bodies",
		DiffCommand:       "go run ./cmd/stagehand diff --candidate-run-id " + runs[2].RunID + " --base-run-id " + runs[0].RunID,
		AssertCommand:     "go run ./cmd/stagehand assert --run-id " + runs[3].RunID + " --assertions " + assertPath,
		SessionInspectCmd: "go run ./cmd/stagehand inspect --session " + demoSessionName + " --show-bodies",
	}, nil
}

func persistDemoRun(ctx context.Context, artifactStore store.ArtifactStore, run recorder.Run) error {
	runRecord := store.RunRecordFromRun(run)
	if err := artifactStore.CreateRun(ctx, runRecord); err != nil {
		return fmt.Errorf("create demo run %q: %w", run.RunID, err)
	}
	for _, interaction := range run.Interactions {
		if err := artifactStore.WriteInteraction(ctx, interaction); err != nil {
			return fmt.Errorf("write demo interaction %q: %w", interaction.InteractionID, err)
		}
	}
	return nil
}

func compareDemoRegression(base recorder.Run, candidate recorder.Run) (RegressionSummary, error) {
	result, err := analysisdiff.Compare(base, candidate, analysisdiff.Options{})
	if err != nil {
		return RegressionSummary{}, err
	}
	summary := result.Summary()
	regression := RegressionSummary{
		BaseRunID:       base.RunID,
		CandidateRunID:  candidate.RunID,
		TotalChanges:    summary.TotalChanges,
		FailingChanges:  summary.FailingChanges,
		ChangedBehavior: "support ticket priority changed after the refund workflow",
	}
	if len(result.FailingChanges()) > 0 {
		first := result.FailingChanges()[0]
		regression.FirstType = string(first.Type)
		regression.FirstService = first.Service
		regression.FirstOperation = first.Operation
	}
	return regression, nil
}

func evaluateUnsafeOrdering(run recorder.Run) (AssertionSummary, error) {
	file, err := analysisassertions.Parse([]byte(unsafeOrderingAssertionsYAML))
	if err != nil {
		return AssertionSummary{}, err
	}
	results, err := analysisassertions.Evaluate(run, file)
	if err != nil {
		return AssertionSummary{}, err
	}
	if len(results) != 1 {
		return AssertionSummary{}, fmt.Errorf("expected one assertion result, got %d", len(results))
	}
	result := results[0]
	summary := AssertionSummary{
		RunID:     run.RunID,
		ID:        result.AssertionID,
		Status:    string(result.Status),
		Message:   result.Message,
		Violation: "refund attempted before lookup_customer resolved customer identity",
	}
	if result.Evidence.Before != nil {
		summary.BeforeID = result.Evidence.Before.InteractionID
	}
	if result.Evidence.After != nil {
		summary.AfterID = result.Evidence.After.InteractionID
	}
	return summary, nil
}

func injectionSummary(run recorder.Run) InjectionSummary {
	toolError := terminalEventData(run.Interactions[1])
	provenance := []string{}
	if section, ok := run.Metadata[injection.MetadataKey].(map[string]any); ok {
		if applied, ok := section["applied"].([]any); ok {
			for _, item := range applied {
				if encoded, err := json.Marshal(item); err == nil {
					provenance = append(provenance, string(encoded))
				}
			}
		}
	}
	return InjectionSummary{
		RunID:        run.RunID,
		Tool:         "lookup_customer",
		ErrorType:    "not_found",
		ErrorClass:   stringValue(toolError["error_class"]),
		Message:      stringValue(toolError["message"]),
		Provenance:   provenance,
		RecoveredVia: "support ticket API created an agent-review ticket",
	}
}

func workflowSummary(run recorder.Run) WorkflowSummary {
	summary := WorkflowSummary{
		RunID:            run.RunID,
		Mode:             string(run.Mode),
		InteractionCount: len(run.Interactions),
	}
	for _, interaction := range run.Interactions {
		switch {
		case interaction.Service == "openai":
			summary.OpenAI = true
		case interaction.Service == "stagehand.tool" && interaction.Operation == "lookup_customer":
			summary.LookupTool = true
		case interaction.Service == "internal-billing":
			summary.BillingAPI = true
		case interaction.Service == "stripe":
			summary.Stripe = true
		case interaction.Service == "support-ticket-api":
			summary.SupportAPI = true
		}
	}
	return summary
}

func labels(interactions []recorder.Interaction) []InteractionLabel {
	items := make([]InteractionLabel, 0, len(interactions))
	for _, interaction := range interactions {
		items = append(items, InteractionLabel{
			Service:   interaction.Service,
			Operation: interaction.Operation,
			Protocol:  string(interaction.Protocol),
		})
	}
	return items
}

func recordRun() recorder.Run {
	return demoRun("run_custom_tool_record", recorder.RunModeRecord, time.Unix(10, 0).UTC(), []recorder.Interaction{
		openAIInteraction("run_custom_tool_record", "int_record_openai", 1),
		lookupToolInteraction("run_custom_tool_record", "int_record_lookup", 2, "", recorder.EventTypeResponseReceived, map[string]any{
			"result": map[string]any{
				"id":    "cus_private_123",
				"email": "customer@scrub.local",
				"tier":  "enterprise",
			},
			"side_effect": "read",
			"replay":      "recorded",
		}),
		billingInteraction("run_custom_tool_record", "int_record_billing", 3, "approved"),
		stripeInteraction("run_custom_tool_record", "int_record_stripe", 4, "succeeded"),
		supportInteraction("run_custom_tool_record", "int_record_support", 5, "normal", "refund completed"),
	})
}

func replayRun() recorder.Run {
	run := recordRun()
	run.RunID = "run_custom_tool_replay"
	run.Mode = recorder.RunModeReplay
	run.StartedAt = time.Unix(20, 0).UTC()
	run.EndedAt = timePtr(time.Unix(21, 0).UTC())
	for idx := range run.Interactions {
		run.Interactions[idx].RunID = run.RunID
		run.Interactions[idx].InteractionID = strings.Replace(run.Interactions[idx].InteractionID, "record", "replay", 1)
		run.Interactions[idx].FallbackTier = recorder.FallbackTierExact
		run.Interactions[idx].FallbackReason = "recorded demo replay"
	}
	return run
}

func candidateRun() recorder.Run {
	return demoRun("run_custom_tool_candidate", recorder.RunModeReplay, time.Unix(30, 0).UTC(), []recorder.Interaction{
		openAIInteraction("run_custom_tool_candidate", "int_candidate_openai", 1),
		lookupToolInteraction("run_custom_tool_candidate", "int_candidate_lookup", 2, "", recorder.EventTypeResponseReceived, map[string]any{
			"result": map[string]any{
				"id":    "cus_private_123",
				"email": "customer@scrub.local",
				"tier":  "enterprise",
			},
			"side_effect": "read",
			"replay":      "recorded",
		}),
		billingInteraction("run_custom_tool_candidate", "int_candidate_billing", 3, "approved"),
		stripeInteraction("run_custom_tool_candidate", "int_candidate_stripe", 4, "succeeded"),
		supportInteraction("run_custom_tool_candidate", "int_candidate_support", 5, "urgent", "refund completed"),
	})
}

func unsafeOrderingRun() recorder.Run {
	return demoRun("run_custom_tool_unsafe_order", recorder.RunModeReplay, time.Unix(40, 0).UTC(), []recorder.Interaction{
		openAIInteraction("run_custom_tool_unsafe_order", "int_unsafe_openai", 1),
		billingInteraction("run_custom_tool_unsafe_order", "int_unsafe_billing", 2, "approved"),
		lookupToolInteraction("run_custom_tool_unsafe_order", "int_unsafe_lookup", 3, "", recorder.EventTypeResponseReceived, map[string]any{
			"result": map[string]any{
				"id":    "cus_private_123",
				"email": "customer@scrub.local",
			},
			"side_effect": "read",
			"replay":      "recorded",
		}),
		stripeInteraction("run_custom_tool_unsafe_order", "int_unsafe_stripe", 4, "succeeded"),
		supportInteraction("run_custom_tool_unsafe_order", "int_unsafe_support", 5, "high", "unsafe ordering escalated"),
	})
}

func injectedToolRun() recorder.Run {
	provenance := injection.Provenance{
		RuleIndex:  0,
		Tool:       "lookup_customer",
		Service:    "stagehand.tool",
		Operation:  "lookup_customer",
		CallNumber: 2,
		NthCall:    2,
		Error:      "not_found",
	}
	run := demoRun("run_custom_tool_injected", recorder.RunModeReplay, time.Unix(50, 0).UTC(), []recorder.Interaction{
		openAIInteraction("run_custom_tool_injected", "int_injected_openai", 1),
		lookupToolInteraction("run_custom_tool_injected", "int_injected_lookup_error", 2, "", recorder.EventTypeError, map[string]any{
			"error_class": "CustomerNotFoundError",
			"message":     "customer missing",
			"side_effect": "read",
			"replay":      "recorded",
			"stagehand_injection": map[string]any{
				"rule_index":  0,
				"tool":        "lookup_customer",
				"service":     "stagehand.tool",
				"operation":   "lookup_customer",
				"call_number": float64(2),
				"nth_call":    float64(2),
				"error":       "not_found",
			},
		}),
		supportInteraction("run_custom_tool_injected", "int_injected_support", 3, "urgent", "customer lookup failed; routed to human review"),
	})
	run.Metadata = injection.AppendProvenance(run.Metadata, provenance)
	return run
}

func demoRun(runID string, mode recorder.RunMode, startedAt time.Time, interactions []recorder.Interaction) recorder.Run {
	endedAt := startedAt.Add(time.Second)
	return recorder.Run{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         version.ArtifactVersion,
		RuntimeVersion:     version.ArtifactVersion,
		ScrubPolicyVersion: "v1",
		RunID:              runID,
		SessionName:        demoSessionName,
		Mode:               mode,
		Status:             recorder.RunStatusComplete,
		StartedAt:          startedAt,
		EndedAt:            &endedAt,
		Interactions:       interactions,
	}
}

func openAIInteraction(runID string, interactionID string, sequence int) recorder.Interaction {
	return baseInteraction(runID, interactionID, sequence, "openai", "chat.completions.create", recorder.ProtocolHTTPS, "POST", "https://api.openai.com/v1/chat/completions", map[string]any{
		"model": "gpt-5.4-mini",
		"messages": []map[string]any{
			{"role": "user", "content": "Refund a duplicate enterprise charge after verifying private customer records."},
		},
	}, map[string]any{
		"status_code": 200,
		"body": map[string]any{
			"decision": "lookup_customer_then_refund",
		},
	}, 18)
}

func lookupToolInteraction(runID string, interactionID string, sequence int, parentID string, terminalType recorder.EventType, terminalData map[string]any) recorder.Interaction {
	interaction := baseInteraction(runID, interactionID, sequence, "stagehand.tool", "lookup_customer", recorder.ProtocolTool, "CALL", "stagehand://tool/lookup_customer", map[string]any{
		"name": "lookup_customer",
		"arguments": map[string]any{
			"email": "customer@scrub.local",
		},
		"side_effect": "read",
		"replay":      "recorded",
	}, terminalData, 7)
	interaction.ParentInteractionID = parentID
	interaction.Events[1].Type = terminalType
	return interaction
}

func billingInteraction(runID string, interactionID string, sequence int, status string) recorder.Interaction {
	return baseInteraction(runID, interactionID, sequence, "internal-billing", "POST /api/refunds", recorder.ProtocolHTTPS, "POST", "https://billing.internal.test/api/refunds", map[string]any{
		"customer_id": "cus_private_123",
		"amount":      float64(1500),
		"reason":      "duplicate",
	}, map[string]any{
		"status_code": 200,
		"body": map[string]any{
			"refund_id": "rf_private_123",
			"status":    status,
		},
	}, 12)
}

func stripeInteraction(runID string, interactionID string, sequence int, status string) recorder.Interaction {
	return baseInteraction(runID, interactionID, sequence, "stripe", "refunds.create", recorder.ProtocolHTTPS, "POST", "https://api.stripe.com/v1/refunds", map[string]any{
		"payment_intent": "pi_private_123",
		"amount":         float64(1500),
	}, map[string]any{
		"status_code": 200,
		"body": map[string]any{
			"id":     "re_private_123",
			"status": status,
		},
	}, 14)
}

func supportInteraction(runID string, interactionID string, sequence int, priority string, note string) recorder.Interaction {
	return baseInteraction(runID, interactionID, sequence, "support-ticket-api", "POST /tickets", recorder.ProtocolHTTPS, "POST", "https://support.internal.test/tickets", map[string]any{
		"customer_id": "cus_private_123",
		"priority":    priority,
		"note":        note,
	}, map[string]any{
		"status_code": 201,
		"body": map[string]any{
			"ticket_id": "ticket_private_123",
			"priority":  priority,
		},
	}, 10)
}

func baseInteraction(runID string, interactionID string, sequence int, service string, operation string, protocol recorder.Protocol, method string, url string, requestBody map[string]any, responseData map[string]any, latency int64) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       service,
		Operation:     operation,
		Protocol:      protocol,
		Request: recorder.Request{
			Method: method,
			URL:    url,
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: requestBody,
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: latency, SimTMS: latency, Type: recorder.EventTypeResponseReceived, Data: responseData},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_custom_tool_demo",
		},
		LatencyMS: latency,
	}
}

func terminalEventData(interaction recorder.Interaction) map[string]any {
	if len(interaction.Events) == 0 {
		return nil
	}
	return interaction.Events[len(interaction.Events)-1].Data
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func sqliteDatabasePath(storagePath string) string {
	if strings.HasSuffix(storagePath, ".db") {
		return storagePath
	}
	return filepath.Join(storagePath, "stagehand.db")
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

const unsafeOrderingAssertionsYAML = `
schema_version: v1alpha1
assertions:
  - id: lookup-before-private-refund
    type: ordering
    before:
      tool: lookup_customer
    after:
      service: internal-billing
      operation: POST /api/refunds
`
