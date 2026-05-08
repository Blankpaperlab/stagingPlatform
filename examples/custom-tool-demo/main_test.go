package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"stagehand/internal/recorder"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestCustomToolDemoPasses(t *testing.T) {
	t.Parallel()

	summary, err := RunCustomToolDemo(context.Background())
	if err != nil {
		t.Fatalf("RunCustomToolDemo() error = %v", err)
	}

	if summary.SessionName != demoSessionName {
		t.Fatalf("SessionName = %q, want %q", summary.SessionName, demoSessionName)
	}
	if summary.Record.InteractionCount != 5 || !summary.Record.OpenAI || !summary.Record.LookupTool || !summary.Record.BillingAPI || !summary.Record.Stripe || !summary.Record.SupportAPI {
		t.Fatalf("record summary = %#v, want full private workflow", summary.Record)
	}
	if summary.Replay.Mode != string(recorder.RunModeReplay) || summary.Replay.InteractionCount != 5 {
		t.Fatalf("replay summary = %#v, want offline replay run", summary.Replay)
	}
	if summary.Regression.TotalChanges != 1 || summary.Regression.FailingChanges != 1 {
		t.Fatalf("regression summary = %#v, want one failing behavior change", summary.Regression)
	}
	if summary.Regression.FirstService != "support-ticket-api" || summary.Regression.FirstOperation != "POST /tickets" {
		t.Fatalf("regression first change = %#v, want support ticket modified", summary.Regression)
	}
	if summary.Assertion.Status != "failed" || !strings.Contains(summary.Assertion.Violation, "before lookup_customer") {
		t.Fatalf("assertion summary = %#v, want unsafe ordering failure", summary.Assertion)
	}
	if summary.Injection.Tool != "lookup_customer" || summary.Injection.ErrorClass != "CustomerNotFoundError" || summary.Injection.Message != "customer missing" {
		t.Fatalf("injection summary = %#v, want typed lookup failure", summary.Injection)
	}
	if len(summary.Injection.Provenance) != 1 || !strings.Contains(summary.Injection.Provenance[0], `"tool":"lookup_customer"`) {
		t.Fatalf("injection provenance = %#v, want lookup_customer provenance", summary.Injection.Provenance)
	}
}

func TestCustomToolDemoPersistsInspectableRuns(t *testing.T) {
	ctx := context.Background()
	storagePath := filepath.Join(t.TempDir(), "runs")

	artifacts, err := PersistCustomToolDemoRuns(ctx, storagePath)
	if err != nil {
		t.Fatalf("PersistCustomToolDemoRuns() error = %v", err)
	}

	if !artifacts.Persisted {
		t.Fatal("Persisted = false, want true")
	}
	if artifacts.RecordRunID != "run_custom_tool_record" || artifacts.ReplayRunID != "run_custom_tool_replay" {
		t.Fatalf("record/replay ids = %q/%q, want custom tool demo ids", artifacts.RecordRunID, artifacts.ReplayRunID)
	}
	if !strings.Contains(artifacts.DiffCommand, "diff --candidate-run-id run_custom_tool_candidate --base-run-id run_custom_tool_record") {
		t.Fatalf("DiffCommand = %q, want runnable diff command", artifacts.DiffCommand)
	}
	if !strings.Contains(artifacts.AssertCommand, "assert --run-id run_custom_tool_unsafe_order") {
		t.Fatalf("AssertCommand = %q, want unsafe ordering assertion command", artifacts.AssertCommand)
	}

	sqliteStore, err := sqlitestore.OpenStore(ctx, sqliteDatabasePath(storagePath))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	record, err := sqliteStore.GetRun(ctx, artifacts.RecordRunID)
	if err != nil {
		t.Fatalf("GetRun(record) error = %v", err)
	}
	injected, err := sqliteStore.GetRun(ctx, artifacts.InjectedRunID)
	if err != nil {
		t.Fatalf("GetRun(injected) error = %v", err)
	}
	latest, err := sqliteStore.GetLatestRunRecord(ctx, demoSessionName)
	if err != nil {
		t.Fatalf("GetLatestRunRecord() error = %v", err)
	}
	if latest.RunID != artifacts.InjectedRunID {
		t.Fatalf("latest run id = %q, want injected run %q", latest.RunID, artifacts.InjectedRunID)
	}
	if record.Interactions[0].Service != "openai" || record.Interactions[1].Protocol != recorder.ProtocolTool {
		t.Fatalf("recorded interactions = %#v, want OpenAI then tool", record.Interactions[:2])
	}
	if injected.Metadata["error_injection"] == nil {
		t.Fatalf("injected metadata = %#v, want error_injection", injected.Metadata)
	}
	if terminalEventData(injected.Interactions[1])["stagehand_injection"] == nil {
		t.Fatalf("injected tool event = %#v, want stagehand_injection", terminalEventData(injected.Interactions[1]))
	}
}

func TestCustomToolDemoUnsafeOrderingAssertionFails(t *testing.T) {
	t.Parallel()

	summary, err := evaluateUnsafeOrdering(unsafeOrderingRun())
	if err != nil {
		t.Fatalf("evaluateUnsafeOrdering() error = %v", err)
	}
	if summary.Status != "failed" {
		t.Fatalf("Status = %q, want failed", summary.Status)
	}
	if !strings.Contains(summary.Message, "no before/after match pair") {
		t.Fatalf("Message = %q, want no before/after match pair", summary.Message)
	}
}

func TestCustomToolDemoReplayFixtureShapeIsStable(t *testing.T) {
	t.Parallel()

	// Fixture-shape guard: this does not exercise SDK replay, it keeps the demo's persisted replay walkthrough stable.
	replayA := canonicalDemoInteractions(replayRun().Interactions)
	replayB := canonicalDemoInteractions(replayRun().Interactions)
	if !reflect.DeepEqual(replayA, replayB) {
		t.Fatalf("canonical replay interactions differ\nA=%#v\nB=%#v", replayA, replayB)
	}
}

func TestCustomToolDemoFixtureSequenceAcrossBoundaries(t *testing.T) {
	t.Parallel()

	// Fixture-shape guard: this verifies the inspect/diff walkthrough timeline encoded by the demo constructors.
	run := recordRun()
	want := []struct {
		sequence  int
		protocol  recorder.Protocol
		service   string
		operation string
	}{
		{1, recorder.ProtocolHTTPS, "openai", "chat.completions.create"},
		{2, recorder.ProtocolTool, "stagehand.tool", "lookup_customer"},
		{3, recorder.ProtocolHTTPS, "internal-billing", "POST /api/refunds"},
		{4, recorder.ProtocolHTTPS, "stripe", "refunds.create"},
		{5, recorder.ProtocolHTTPS, "support-ticket-api", "POST /tickets"},
	}
	if len(run.Interactions) != len(want) {
		t.Fatalf("interaction count = %d, want %d", len(run.Interactions), len(want))
	}
	seen := map[int]bool{}
	for idx, expected := range want {
		got := run.Interactions[idx]
		if seen[got.Sequence] {
			t.Fatalf("duplicate sequence %d in interactions: %#v", got.Sequence, run.Interactions)
		}
		seen[got.Sequence] = true
		if got.Sequence != expected.sequence || got.Protocol != expected.protocol || got.Service != expected.service || got.Operation != expected.operation {
			t.Fatalf("interaction %d = seq=%d protocol=%s service=%s operation=%s, want %#v", idx, got.Sequence, got.Protocol, got.Service, got.Operation, expected)
		}
	}
}

func canonicalDemoInteractions(interactions []recorder.Interaction) []map[string]any {
	canonical := make([]map[string]any, 0, len(interactions))
	for _, interaction := range interactions {
		canonical = append(canonical, map[string]any{
			"sequence":      interaction.Sequence,
			"service":       interaction.Service,
			"operation":     interaction.Operation,
			"protocol":      interaction.Protocol,
			"request_body":  canonicalJSON(interaction.Request.Body),
			"terminal_type": terminalEventType(interaction),
			"terminal_data": canonicalJSON(terminalEventData(interaction)),
		})
	}
	return canonical
}

func canonicalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func terminalEventType(interaction recorder.Interaction) recorder.EventType {
	if len(interaction.Events) == 0 {
		return ""
	}
	return interaction.Events[len(interaction.Events)-1].Type
}
