package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
	"stagehand/internal/version"
)

func TestRunWritesRootHelpWithNoArgs(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := run(nil, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "stagehand "+version.CLIVersion) {
		t.Fatalf("stdout = %q, want CLI version banner", stdout.String())
	}
	if !strings.Contains(stdout.String(), "record") {
		t.Fatalf("stdout = %q, want record command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "replay") {
		t.Fatalf("stdout = %q, want replay command in help", stdout.String())
	}
	if !strings.Contains(stdout.String(), "inspect") {
		t.Fatalf("stdout = %q, want inspect command in help", stdout.String())
	}
}

func TestRunRecordRequiresSession(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"record"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record) expected --session validation error")
	}
	if !strings.Contains(err.Error(), "record requires --session") {
		t.Fatalf("run(record) error = %v", err)
	}
}

func TestRunRecordRequiresCommand(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(filepath.Join(workdir, ".stagehand", "runs"))+"\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"record", "--session", "onboarding-flow", "--config", configPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record) expected managed command validation error")
	}
	if !strings.Contains(err.Error(), "record requires a command after --") {
		t.Fatalf("run(record) error = %v", err)
	}
}

func TestRunRecordRunsManagedCommandPersistsInteractionsAndEmitsJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "onboarding-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(record) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[recordResult](t, stdout.Bytes())
	if result.Mode != "record" {
		t.Fatalf("result.Mode = %q, want record", result.Mode)
	}
	if result.SessionName != "onboarding-flow" {
		t.Fatalf("result.SessionName = %q, want onboarding-flow", result.SessionName)
	}
	if result.Status != "complete" {
		t.Fatalf("result.Status = %q, want complete", result.Status)
	}
	if result.InteractionCount != 1 {
		t.Fatalf("result.InteractionCount = %d, want 1", result.InteractionCount)
	}
	if result.EmptyRun {
		t.Fatal("result.EmptyRun = true, want false for recorded interaction")
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	gotRun, err := sqliteStore.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Mode != recorder.RunModeRecord {
		t.Fatalf("GetRun().Mode = %q, want %q", gotRun.Mode, recorder.RunModeRecord)
	}
	if gotRun.Status != recorder.RunStatusComplete {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, recorder.RunStatusComplete)
	}
	if len(gotRun.Interactions) != 1 {
		t.Fatalf("len(GetRun().Interactions) = %d, want 1", len(gotRun.Interactions))
	}

	interaction := gotRun.Interactions[0]
	if interaction.RunID != result.RunID {
		t.Fatalf("Interaction.RunID = %q, want %q", interaction.RunID, result.RunID)
	}
	if _, ok := interaction.Request.Headers["authorization"]; ok {
		t.Fatal("authorization header should have been scrubbed before persistence")
	}
	if interaction.ScrubReport.ScrubPolicyVersion != "v1" {
		t.Fatalf(
			"Interaction.ScrubReport.ScrubPolicyVersion = %q, want v1",
			interaction.ScrubReport.ScrubPolicyVersion,
		)
	}
}

func TestRunRecordTreatsEmptyCaptureBundleAsCompleteEmptyRun(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "empty-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-write-empty-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(record empty) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[recordResult](t, stdout.Bytes())
	if result.Status != "complete" {
		t.Fatalf("result.Status = %q, want complete", result.Status)
	}
	if result.InteractionCount != 0 {
		t.Fatalf("result.InteractionCount = %d, want 0", result.InteractionCount)
	}
	if !result.EmptyRun {
		t.Fatal("result.EmptyRun = false, want true")
	}

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	gotRun, err := sqliteStore.GetRun(context.Background(), result.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if gotRun.Status != recorder.RunStatusComplete {
		t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, recorder.RunStatusComplete)
	}
	if len(gotRun.Interactions) != 0 {
		t.Fatalf("len(GetRun().Interactions) = %d, want 0", len(gotRun.Interactions))
	}
}

func TestRunRecordSurfacesCaptureBundleWriteFailure(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	args := []string{
		"record",
		"--session", "failing-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("record-fail-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(record failing helper) expected error")
	}
	if !strings.Contains(err.Error(), `run managed command "`) {
		t.Fatalf("run(record failing helper) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "simulated capture bundle write failure:") {
		t.Fatalf("stderr missing concrete capture bundle failure\nstderr=%s", stderr.String())
	}
}

func TestSQLiteDatabasePathUsesFileWhenAlreadyFileLike(t *testing.T) {
	got := sqliteDatabasePath(filepath.Join("tmp", "custom.sqlite"))
	want := filepath.Join("tmp", "custom.sqlite")
	if got != want {
		t.Fatalf("sqliteDatabasePath() = %q, want %q", got, want)
	}
}

func TestRunReplayRequiresSingleSelector(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run([]string{"replay"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(replay) expected selector validation error")
	}
	if !strings.Contains(err.Error(), "exactly one of --run-id or --session") {
		t.Fatalf("run(replay) error = %v", err)
	}
}

func TestRunReplayRequiresCommand(t *testing.T) {
	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(filepath.Join(workdir, ".stagehand", "runs"))+"\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"replay", "--run-id", "run_demo", "--config", configPath}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(replay) expected managed command validation error")
	}
	if !strings.Contains(err.Error(), "replay requires a command after --") {
		t.Fatalf("run(replay) error = %v", err)
	}
}

func TestRunReplayRunsManagedCommandStoresReplayRunAndEmitsJSON(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	sourceRunID := seedReplayRun(t, sqliteStore, "onboarding-flow")

	args := []string{
		"replay",
		"--run-id", sourceRunID,
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("replay-consume-seed-and-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(replay) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[replayCommandResult](t, stdout.Bytes())
	if result.Mode != "replay" {
		t.Fatalf("result.Mode = %q, want replay", result.Mode)
	}
	if result.SourceRunID != sourceRunID {
		t.Fatalf("result.SourceRunID = %q, want %q", result.SourceRunID, sourceRunID)
	}
	if result.ReplayRunID == "" {
		t.Fatal("result.ReplayRunID is empty")
	}
	if result.Status != "complete" {
		t.Fatalf("result.Status = %q, want complete", result.Status)
	}
	if result.ReplayInteractionCount != 1 {
		t.Fatalf("result.ReplayInteractionCount = %d, want 1", result.ReplayInteractionCount)
	}

	replayRun, err := sqliteStore.GetRun(context.Background(), result.ReplayRunID)
	if err != nil {
		t.Fatalf("GetRun(replay) error = %v", err)
	}
	if replayRun.Mode != recorder.RunModeReplay {
		t.Fatalf("replayRun.Mode = %q, want %q", replayRun.Mode, recorder.RunModeReplay)
	}
	if len(replayRun.Interactions) != 1 {
		t.Fatalf("len(replayRun.Interactions) = %d, want 1", len(replayRun.Interactions))
	}
}

func TestRunReplayLoadsLatestCompleteRunBySession(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	baseStartedAt := time.Date(2026, time.April, 22, 10, 0, 0, 0, time.UTC)
	completeRunID := seedReplayRunAt(t, sqliteStore, "onboarding-flow", baseStartedAt)
	seedCorruptedReplayCandidate(t, sqliteStore, "onboarding-flow", baseStartedAt.Add(10*time.Minute))

	args := []string{
		"replay",
		"--session", "onboarding-flow",
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("replay-consume-seed-and-write-capture")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(args, &stdout, &stderr); err != nil {
		t.Fatalf("run(replay by session) error = %v\nstderr=%s", err, stderr.String())
	}

	result := decodeJSONOutput[replayCommandResult](t, stdout.Bytes())
	if result.SourceRunID != completeRunID {
		t.Fatalf("result.SourceRunID = %q, want latest complete run %q", result.SourceRunID, completeRunID)
	}
}

func TestRunReplaySurfacesControlledFailureForStoredFailureInteraction(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	t.Setenv(envStagehandMasterKey, strings.Repeat("ab", 32))

	workdir := t.TempDir()
	configPath := filepath.Join(workdir, "stagehand.yml")
	storagePath := filepath.Join(workdir, ".stagehand", "runs")
	writeFile(t, configPath, "schema_version: v1alpha1\nrecord:\n  storage_path: "+toSlash(storagePath)+"\n")

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(storagePath, "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	sourceRunID := seedReplayFailureRun(t, sqliteStore, "failure-flow")

	args := []string{
		"replay",
		"--run-id", sourceRunID,
		"--config", configPath,
		"--",
	}
	args = append(args, helperCommand("replay-fail-on-terminal-error")...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = run(args, &stdout, &stderr)
	if err == nil {
		t.Fatal("run(replay failing source) expected error")
	}
	if !strings.Contains(err.Error(), `run managed command "`) {
		t.Fatalf("run(replay failing source) error = %v", err)
	}
	if !strings.Contains(stderr.String(), "controlled replay failure: matched source interaction ended with error") {
		t.Fatalf("stderr missing controlled replay failure\nstderr=%s", stderr.String())
	}
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	mode := helperModeFromArgs(os.Args)
	switch mode {
	case "record-write-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: helperCapturedInteractions(),
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	case "record-write-empty-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		writeFile(t, capturePath, "{\n  \"bundle_version\": \"v1alpha1\",\n  \"metadata\": {\n    \"mode\": \"record\",\n    \"session\": \"empty-flow\"\n  },\n  \"interactions\": []\n}\n")
		os.Exit(0)
	case "record-fail-write-capture":
		capturePath := mustEnv(t, envStagehandCaptureOut)
		if err := os.Mkdir(capturePath, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: helperCapturedInteractions(),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "simulated capture bundle write failure: %v\n", err)
			os.Exit(6)
		}
		os.Exit(0)
	case "replay-consume-seed-and-write-capture":
		seedPath := mustEnv(t, envStagehandReplayInput)
		capturePath := mustEnv(t, envStagehandCaptureOut)
		bundle, err := loadInteractionBundle(seedPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		if err := writeInteractionBundle(capturePath, interactionBundle{
			BundleVersion: interactionBundleVersion,
			Metadata: map[string]any{
				"mode":    os.Getenv(envStagehandMode),
				"session": os.Getenv(envStagehandSession),
			},
			Interactions: bundle.Interactions,
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		os.Exit(0)
	case "replay-fail-on-terminal-error":
		seedPath := mustEnv(t, envStagehandReplayInput)
		if bundle, err := loadInteractionBundle(seedPath); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(7)
		} else {
			for _, interaction := range bundle.Interactions {
				for _, event := range interaction.Events {
					if event.Type == recorder.EventTypeError || event.Type == recorder.EventTypeTimeout {
						fmt.Fprintf(os.Stderr, "controlled replay failure: matched source interaction ended with %s\n", event.Type)
						os.Exit(7)
					}
				}
			}
		}
		fmt.Fprintln(os.Stderr, "controlled replay failure helper expected a terminal failure interaction")
		os.Exit(7)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q\n", mode)
		os.Exit(5)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func toSlash(path string) string {
	return strings.ReplaceAll(path, `\`, `/`)
}

func seedReplayRun(t *testing.T, artifactStore store.ArtifactStore, session string) string {
	t.Helper()

	return seedReplayRunAt(t, artifactStore, session, time.Now().UTC())
}

func seedReplayRunAt(
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
	run.StartedAt = startedAt
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	interaction := recorder.Interaction{
		RunID:         run.RunID,
		InteractionID: "int_" + run.RunID,
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Body: map[string]any{
				"model":    "gpt-5.4",
				"messages": []map[string]string{{"role": "user", "content": "say hello"}},
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{Sequence: 2, TMS: 1, SimTMS: 1, Type: recorder.EventTypeResponseReceived},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fixture",
		},
	}
	if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}
	if err := finalizeRun(context.Background(), artifactStore, run, nil); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}

	return run.RunID
}

func seedCorruptedReplayCandidate(
	t *testing.T,
	artifactStore store.ArtifactStore,
	session string,
	startedAt time.Time,
) {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	run.RunID = "run_corrupted_latest"
	run.StartedAt = startedAt
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun(corrupted) error = %v", err)
	}
	endedAt := startedAt.Add(30 * time.Second)
	run.EndedAt = &endedAt
	run.Status = store.RunLifecycleStatusCorrupted
	run.IntegrityIssues = []recorder.IntegrityIssue{
		{
			Code:    recorder.IntegrityIssueRecorderShutdown,
			Message: "simulated failure",
		},
	}
	if err := artifactStore.UpdateRun(context.Background(), run); err != nil {
		t.Fatalf("UpdateRun(corrupted) error = %v", err)
	}
}

func seedReplayFailureRun(t *testing.T, artifactStore store.ArtifactStore, session string) string {
	t.Helper()

	run, err := newRunRecordForMode(session, minimalConfig(), recorder.RunModeRecord)
	if err != nil {
		t.Fatalf("newRunRecordForMode() error = %v", err)
	}
	if err := artifactStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	interaction := recorder.Interaction{
		RunID:         run.RunID,
		InteractionID: "int_" + run.RunID,
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Body: map[string]any{
				"model":    "gpt-5.4",
				"messages": []map[string]string{{"role": "user", "content": "cause failure"}},
			},
		},
		Events: []recorder.Event{
			{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
			{
				Sequence: 2,
				TMS:      3,
				SimTMS:   3,
				Type:     recorder.EventTypeError,
				Data: map[string]any{
					"message": "rate limited",
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_fixture",
		},
	}
	if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}
	if err := finalizeRun(context.Background(), artifactStore, run, nil); err != nil {
		t.Fatalf("finalizeRun() error = %v", err)
	}

	return run.RunID
}

func minimalConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.Scrub.PolicyVersion = "v1"
	return cfg
}

func helperCommand(mode string) []string {
	return []string{os.Args[0], "-test.run=TestHelperProcess", "--", mode}
}

func helperModeFromArgs(args []string) string {
	for idx, arg := range args {
		if arg == "--" && idx+1 < len(args) {
			return args[idx+1]
		}
	}
	return ""
}

func helperCapturedInteractions() []recorder.Interaction {
	return []recorder.Interaction{
		{
			RunID:         "run_helper_source",
			InteractionID: "int_helper_001",
			Sequence:      1,
			Service:       "openai",
			Operation:     "chat.completions.create",
			Protocol:      recorder.ProtocolHTTPS,
			Request: recorder.Request{
				URL:    "https://api.openai.com/v1/chat/completions?email=alice@example.com",
				Method: "POST",
				Headers: map[string][]string{
					"authorization": {"Bearer sk_live_test_secret"},
					"content-type":  {"application/json"},
				},
				Body: map[string]any{
					"messages": []map[string]string{{"role": "user", "content": "contact alice@example.com"}},
					"model":    "gpt-5.4",
				},
			},
			Events: []recorder.Event{
				{Sequence: 1, TMS: 0, SimTMS: 0, Type: recorder.EventTypeRequestSent},
				{
					Sequence: 2,
					TMS:      12,
					SimTMS:   12,
					Type:     recorder.EventTypeResponseReceived,
					Data: map[string]any{
						"status_code": 200,
						"body": map[string]any{
							"id": "resp_123",
						},
					},
				},
			},
			ScrubReport: recorder.ScrubReport{
				ScrubPolicyVersion: "v0-unredacted",
				SessionSaltID:      "salt_pending",
			},
			LatencyMS: 12,
		},
	}
}

func mustEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if strings.TrimSpace(value) == "" {
		t.Fatalf("%s is empty", key)
	}
	return value
}

func decodeJSONOutput[T any](t *testing.T, data []byte) T {
	t.Helper()

	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\npayload=%s", err, string(data))
	}
	return value
}
