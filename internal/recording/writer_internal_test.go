package recording

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/scrub"
	"stagehand/internal/scrub/session_salt"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestWriterReusesCompiledPipelineWithinSameSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openInternalStore(t)
	defer sqliteStore.Close()

	run := internalRunRecord("run_writer_cache_001", "same-session")
	if err := sqliteStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	writer := newInternalWriter(t, sqliteStore)
	var buildCount atomic.Int32
	originalFactory := writer.pipelineFactory
	writer.pipelineFactory = func(opts scrub.Options) (*scrub.Pipeline, error) {
		buildCount.Add(1)
		return originalFactory(opts)
	}

	if _, err := writer.PersistInteraction(ctx, internalInteraction(run.RunID, "int_writer_cache_001", 1)); err != nil {
		t.Fatalf("first PersistInteraction() error = %v", err)
	}
	if _, err := writer.PersistInteraction(ctx, internalInteraction(run.RunID, "int_writer_cache_002", 2)); err != nil {
		t.Fatalf("second PersistInteraction() error = %v", err)
	}

	if got := buildCount.Load(); got != 1 {
		t.Fatalf("pipeline build count = %d, want 1 for same-session reuse", got)
	}
}

func TestWriterBuildsDistinctPipelinesAcrossSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqliteStore := openInternalStore(t)
	defer sqliteStore.Close()

	firstRun := internalRunRecord("run_writer_cache_a", "session-a")
	secondRun := internalRunRecord("run_writer_cache_b", "session-b")
	if err := sqliteStore.CreateRun(ctx, firstRun); err != nil {
		t.Fatalf("CreateRun(first) error = %v", err)
	}
	if err := sqliteStore.CreateRun(ctx, secondRun); err != nil {
		t.Fatalf("CreateRun(second) error = %v", err)
	}

	writer := newInternalWriter(t, sqliteStore)
	var buildCount atomic.Int32
	originalFactory := writer.pipelineFactory
	writer.pipelineFactory = func(opts scrub.Options) (*scrub.Pipeline, error) {
		buildCount.Add(1)
		return originalFactory(opts)
	}

	if _, err := writer.PersistInteraction(ctx, internalInteraction(firstRun.RunID, "int_writer_cache_a", 1)); err != nil {
		t.Fatalf("PersistInteraction(first) error = %v", err)
	}
	if _, err := writer.PersistInteraction(ctx, internalInteraction(secondRun.RunID, "int_writer_cache_b", 1)); err != nil {
		t.Fatalf("PersistInteraction(second) error = %v", err)
	}

	if got := buildCount.Load(); got != 2 {
		t.Fatalf("pipeline build count = %d, want 2 for distinct sessions", got)
	}
}

func openInternalStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	return sqliteStore
}

func newInternalWriter(t *testing.T, artifactStore store.ArtifactStore) *Writer {
	t.Helper()

	cfg := config.DefaultConfig()
	saltManager, err := session_salt.NewManager(artifactStore, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	writer, err := NewWriter(WriterOptions{
		Store:       artifactStore,
		SaltManager: saltManager,
		ScrubConfig: cfg.Scrub,
	})
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}

	return writer
}

func internalRunRecord(runID, sessionName string) store.RunRecord {
	startedAt := time.Date(2026, time.April, 23, 10, 0, 0, 0, time.UTC)
	return store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              runID,
		SessionName:        sessionName,
		Mode:               recorder.RunModeRecord,
		Status:             store.RunLifecycleStatusRunning,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
	}
}

func internalInteraction(runID, interactionID string, sequence int) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: map[string]any{
				"notes": "hello",
			},
		},
		Events: []recorder.Event{
			{
				Sequence: 1,
				TMS:      0,
				SimTMS:   0,
				Type:     recorder.EventTypeRequestSent,
			},
			{
				Sequence: 2,
				TMS:      10,
				SimTMS:   10,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"status_code": 200,
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "placeholder",
			SessionSaltID:      "placeholder",
		},
		LatencyMS: 10,
	}
}
