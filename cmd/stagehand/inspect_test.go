package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestRunInspectRequiresSingleSelector(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(filepath.Join(workdir, ".stagehand", "runs"))+"\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"inspect", "--config", configPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(inspect) expected selector validation error")
	}
	if !strings.Contains(err.Error(), "inspect requires exactly one of --run-id or --session") {
		t.Fatalf("run(inspect) error = %v", err)
	}
}

func TestRunInspectRendersNestedInteractionsAndMetadata(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runID := seedInspectableRun(t, sqliteStore, "inspect-flow", store.RunLifecycleStatusCorrupted, time.Date(2026, time.April, 22, 14, 0, 0, 0, time.UTC))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--run-id", runID, "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect) error = %v", err)
	}

	output := stdout.String()
	for _, expected := range []string{
		"Run: " + runID,
		"Session: inspect-flow",
		"Status: CORRUPTED",
		"Integrity Issues:",
		"- recorder_shutdown: simulated inspect failure",
		"- [1] openai chat.completions.create protocol=https latency=24ms fallback=exact",
		"  - [2] stripe customers.create protocol=https latency=11ms fallback=none",
		"request: POST https://api.openai.com/v1/chat/completions",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("inspect output missing %q\noutput:\n%s", expected, output)
		}
	}

	if strings.Contains(output, "request body:") {
		t.Fatalf("inspect output should not expand bodies without --show-bodies\noutput:\n%s", output)
	}
}

func TestRunInspectShowBodiesExpandsRequestAndEventData(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runID := seedInspectableRun(t, sqliteStore, "inspect-flow", store.RunLifecycleStatusComplete, time.Date(2026, time.April, 22, 15, 0, 0, 0, time.UTC))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--run-id", runID, "--config", configPath, "--show-bodies"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect --show-bodies) error = %v", err)
	}

	output := stdout.String()
	for _, expected := range []string{
		"Status: COMPLETE",
		"request body:",
		"\"model\": \"gpt-5.4\"",
		"data:",
		"\"status_code\": 200",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("inspect --show-bodies output missing %q\noutput:\n%s", expected, output)
		}
	}
}

func TestRunInspectLoadsLatestRunRecordBySession(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	olderStartedAt := time.Date(2026, time.April, 22, 16, 0, 0, 0, time.UTC)
	_ = seedInspectableRun(t, sqliteStore, "session-inspect", store.RunLifecycleStatusComplete, olderStartedAt)
	latestRunID := seedInspectableRun(t, sqliteStore, "session-inspect", store.RunLifecycleStatusCorrupted, olderStartedAt.Add(10*time.Minute))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--session", "session-inspect", "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect --session) error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Run: "+latestRunID) {
		t.Fatalf("inspect --session did not load latest run record\noutput:\n%s", output)
	}
	if !strings.Contains(output, "Status: CORRUPTED") {
		t.Fatalf("inspect --session did not render corrupted latest run\noutput:\n%s", output)
	}
}

func TestRunInspectRendersEmptyRunClearly(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runID := seedInspectableEmptyRun(
		t,
		sqliteStore,
		"empty-inspect",
		time.Date(2026, time.April, 22, 17, 0, 0, 0, time.UTC),
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--run-id", runID, "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect empty) error = %v", err)
	}

	output := stdout.String()
	for _, expected := range []string{
		"Run: " + runID,
		"Status: COMPLETE",
		"Interactions: 0",
		"Interactions:\n(none)\n",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("inspect empty output missing %q\noutput:\n%s", expected, output)
		}
	}
}

func TestRunInspectRendersIncompleteRunClearly(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runID := seedInspectableRun(
		t,
		sqliteStore,
		"incomplete-inspect",
		store.RunLifecycleStatusIncomplete,
		time.Date(2026, time.April, 22, 18, 0, 0, 0, time.UTC),
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--run-id", runID, "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect incomplete) error = %v", err)
	}

	output := stdout.String()
	for _, expected := range []string{
		"Run: " + runID,
		"Status: INCOMPLETE",
		"Integrity Issues:",
		"- missing_end_state: simulated missing terminal event",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("inspect incomplete output missing %q\noutput:\n%s", expected, output)
		}
	}
}

func TestRunInspectShowsMappedGenericHTTPServiceLabels(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, strings.Join([]string{
		"schema_version: v1alpha1",
		"record:",
		"  storage_path: " + toSlash(storagePath),
		"services:",
		"  - name: internal-crm",
		"    type: api",
		"    match:",
		"      host: crm.internal.acme.com",
		"      path_prefix: /v1",
		"    replay:",
		"      mode: generic_http",
		"      allowed_tiers: [0, 1]",
		"",
	}, "\n"))

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runRecord, err := newRunRecordForMode("mapped-inspect", minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	runRecord.RunID = "run_inspect_mapped_service"
	runRecord.StartedAt = time.Date(2026, time.May, 7, 10, 0, 0, 0, time.UTC)
	endedAt := runRecord.StartedAt.Add(5 * time.Second)
	runRecord.EndedAt = &endedAt
	runRecord.Status = store.RunLifecycleStatusComplete
	if err := sqliteStore.CreateRun(context.Background(), runRecord); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	interaction := recorder.Interaction{
		RunID:         runRecord.RunID,
		InteractionID: "int_mapped_crm_001",
		Sequence:      1,
		Service:       "internal-crm",
		Operation:     "POST /v1/customers/search",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://crm.internal.acme.com/v1/customers/search",
			Method: "POST",
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: map[string]any{
				"email": "customer@scrub.local",
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      5,
				SimTMS:   5,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status_code": 200,
					"body":        map[string]any{"ok": true},
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_inspect",
		},
		LatencyMS: 5,
	}
	if err := sqliteStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--run-id", runRecord.RunID, "--config", configPath}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect) error = %v", err)
	}

	output := stdout.String()
	for _, expected := range []string{
		"- [1] internal-crm POST /v1/customers/search protocol=https latency=5ms fallback=none",
		"request: POST https://crm.internal.acme.com/v1/customers/search",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("inspect output missing %q\noutput:\n%s", expected, output)
		}
	}
}

func TestRunInspectRendersToolInteractions(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	runRecord, err := newRunRecordForMode("tool-inspect", minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	runRecord.RunID = "run_inspect_tool"
	runRecord.StartedAt = time.Date(2026, time.May, 8, 9, 0, 0, 0, time.UTC)
	endedAt := runRecord.StartedAt.Add(2 * time.Second)
	runRecord.EndedAt = &endedAt
	runRecord.Status = store.RunLifecycleStatusComplete
	if err := sqliteStore.CreateRun(context.Background(), runRecord); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	parentID := "int_tool_lookup"
	parent := recorder.Interaction{
		RunID:         runRecord.RunID,
		InteractionID: parentID,
		Sequence:      1,
		Service:       "stagehand.tool",
		Operation:     "lookup_customer",
		Protocol:      recorder.ProtocolTool,
		Request: recorder.Request{
			URL:    "stagehand://tool/lookup_customer",
			Method: "CALL",
			Body: map[string]any{
				"name":        "lookup_customer",
				"arguments":   map[string]any{"email": "user_scrubbed@scrub.local"},
				"side_effect": "read",
				"replay":      "recorded",
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      6,
				SimTMS:   6,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"result":      map[string]any{"id": "cus_123"},
					"side_effect": "read",
					"replay":      "recorded",
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_inspect",
		},
		LatencyMS: 6,
	}
	child := recorder.Interaction{
		RunID:               runRecord.RunID,
		InteractionID:       "int_tool_normalize",
		ParentInteractionID: parentID,
		Sequence:            2,
		Service:             "stagehand.tool",
		Operation:           "normalize_customer",
		Protocol:            recorder.ProtocolTool,
		Request: recorder.Request{
			URL:    "stagehand://tool/normalize_customer",
			Method: "CALL",
			Body: map[string]any{
				"name":        "normalize_customer",
				"arguments":   map[string]any{"id": "cus_123"},
				"side_effect": "read",
				"replay":      "recorded",
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 7, SimTMS: 7, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      9,
				SimTMS:   9,
				Type:     recorder.EventTypeError,
				Data: map[string]any{
					"error_class": "ValueError",
					"message":     "bad customer",
					"side_effect": "read",
					"replay":      "recorded",
					"stagehand_injection": map[string]any{
						"name": "normalize failure",
						"tool": "normalize_customer",
					},
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_inspect",
		},
		LatencyMS: 2,
	}
	for _, interaction := range []recorder.Interaction{parent, child} {
		if err := sqliteStore.WriteInteraction(context.Background(), interaction); err != nil {
			t.Fatalf("WriteInteraction(%q) error = %v", interaction.InteractionID, err)
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"inspect", "--run-id", runRecord.RunID, "--config", configPath, "--show-bodies"}, &stdout, &stderr); err != nil {
		t.Fatalf("run(inspect --show-bodies) error = %v", err)
	}

	output := stdout.String()
	for _, expected := range []string{
		"- [1] tool lookup_customer protocol=tool side_effect=read latency=6ms fallback=none",
		"tool args:",
		"\"email\": \"user_scrubbed@scrub.local\"",
		"tool result:",
		"\"id\": \"cus_123\"",
		"  - [2] tool normalize_customer protocol=tool side_effect=read latency=2ms fallback=none",
		"tool error:",
		"\"error_class\": \"ValueError\"",
		"\"stagehand_injection\":",
		"\"name\": \"normalize failure\"",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("inspect tool output missing %q\noutput:\n%s", expected, output)
		}
	}
}

func seedInspectableRun(
	t *testing.T,
	artifactStore store.ArtifactStore,
	session string,
	status store.RunLifecycleStatus,
	startedAt time.Time,
) string {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	run.RunID = "run_inspect_" + strings.ToLower(string(status)) + "_" + startedAt.Format("150405")
	run.StartedAt = startedAt
	if status != store.RunLifecycleStatusRunning {
		endedAt := startedAt.Add(45 * time.Second)
		run.EndedAt = &endedAt
	}
	run.Status = status
	if status == store.RunLifecycleStatusCorrupted {
		run.IntegrityIssues = []recorder.IntegrityIssue{
			{
				Code:    recorder.IntegrityIssueRecorderShutdown,
				Message: "simulated inspect failure",
			},
		}
	}
	if status == store.RunLifecycleStatusIncomplete {
		run.IntegrityIssues = []recorder.IntegrityIssue{
			{
				Code:    recorder.IntegrityIssueMissingEndState,
				Message: "simulated missing terminal event",
			},
		}
	}

	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	parentID := "int_" + run.RunID + "_001"
	parent := recorder.Interaction{
		RunID:         run.RunID,
		InteractionID: parentID,
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Streaming:     true,
		FallbackTier:  recorder.FallbackTierExact,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: map[string]any{
				"model": "gpt-5.4",
				"messages": []map[string]string{
					{"role": "user", "content": "hello"},
				},
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{
				Sequence:            2,
				TMS:                 9,
				SimTMS:              9,
				Type:                recorder.EventTypeToolCallStart,
				NestedInteractionID: "int_" + run.RunID + "_002",
			},
			{
				Sequence: 3,
				TMS:      24,
				SimTMS:   24,
				Type:     recorder.EventTypeStreamEnd,
				Data: map[string]any{
					"finish_reason": "tool_calls",
					"status_code":   200,
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_inspect",
		},
		LatencyMS: 24,
	}

	child := recorder.Interaction{
		RunID:               run.RunID,
		InteractionID:       "int_" + run.RunID + "_002",
		ParentInteractionID: parentID,
		Sequence:            2,
		Service:             "stripe",
		Operation:           "customers.create",
		Protocol:            recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.stripe.com/v1/customers",
			Method: "POST",
			Body: map[string]any{
				"email": "customer@scrub.local",
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 10, SimTMS: 10, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      21,
				SimTMS:   21,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status_code": 200,
					"id":          "cus_123",
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_inspect",
		},
		LatencyMS: 11,
	}

	for _, interaction := range []recorder.Interaction{parent, child} {
		if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
			t.Fatalf("WriteInteraction(%q) error = %v", interaction.InteractionID, err)
		}
	}

	return run.RunID
}

func seedInspectableEmptyRun(
	t *testing.T,
	artifactStore store.ArtifactStore,
	session string,
	startedAt time.Time,
) string {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	run.RunID = "run_inspect_empty_" + startedAt.Format("150405")
	run.StartedAt = startedAt
	endedAt := startedAt.Add(10 * time.Second)
	run.EndedAt = &endedAt
	run.Status = store.RunLifecycleStatusComplete

	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	return run.RunID
}
