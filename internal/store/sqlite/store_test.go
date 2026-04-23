package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"stagehand/internal/recorder"
	"stagehand/internal/store"
)

func TestStoreCreateGetAndUpdateRun(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	gotRun, err := sqliteStore.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}

	if gotRun.RunID != run.RunID {
		t.Fatalf("GetRun().RunID = %q, want %q", gotRun.RunID, run.RunID)
	}

	updated := run
	updated.AgentVersion = "agent-v2"
	updated.GitSHA = "def456"

	if err := sqliteStore.UpdateRun(context.Background(), updated); err != nil {
		t.Fatalf("UpdateRun() error = %v", err)
	}

	gotUpdatedRun, err := sqliteStore.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("GetRun() after update error = %v", err)
	}

	if gotUpdatedRun.AgentVersion != updated.AgentVersion {
		t.Fatalf("GetRun().AgentVersion = %q, want %q", gotUpdatedRun.AgentVersion, updated.AgentVersion)
	}

	if gotUpdatedRun.GitSHA != updated.GitSHA {
		t.Fatalf("GetRun().GitSHA = %q, want %q", gotUpdatedRun.GitSHA, updated.GitSHA)
	}
}

func TestStoreSupportsRunningRunLifecycle(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunningRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	gotRecord, err := sqliteStore.GetRunRecord(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("GetRunRecord() error = %v", err)
	}

	if gotRecord.Status != store.RunLifecycleStatusRunning {
		t.Fatalf("GetRunRecord().Status = %q, want %q", gotRecord.Status, store.RunLifecycleStatusRunning)
	}

	if _, err := sqliteStore.GetRun(context.Background(), run.RunID); err == nil {
		t.Fatal("GetRun() expected failure for running run")
	}

	interaction := validInteraction(run.RunID)
	if err := sqliteStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}

	interactions, err := sqliteStore.ListInteractions(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("ListInteractions() error = %v", err)
	}

	if len(interactions) != 1 {
		t.Fatalf("len(ListInteractions()) = %d, want 1", len(interactions))
	}
}

func TestStoreWritesAndReadsInteractions(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	interaction := validInteraction(run.RunID)
	if err := sqliteStore.WriteInteraction(context.Background(), interaction); err != nil {
		t.Fatalf("WriteInteraction() error = %v", err)
	}

	interactions, err := sqliteStore.ListInteractions(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("ListInteractions() error = %v", err)
	}

	if len(interactions) != 1 {
		t.Fatalf("len(ListInteractions()) = %d, want 1", len(interactions))
	}

	if interactions[0].InteractionID != interaction.InteractionID {
		t.Fatalf("InteractionID = %q, want %q", interactions[0].InteractionID, interaction.InteractionID)
	}

	if len(interactions[0].Events) != len(interaction.Events) {
		t.Fatalf("len(Events) = %d, want %d", len(interactions[0].Events), len(interaction.Events))
	}

	gotRun, err := sqliteStore.GetRun(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}

	if len(gotRun.Interactions) != 1 {
		t.Fatalf("len(GetRun().Interactions) = %d, want 1", len(gotRun.Interactions))
	}
}

func TestStoreInteractionWriteIsAtomic(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	badInteraction := validInteraction(run.RunID)
	badInteraction.Events[1].Sequence = badInteraction.Events[0].Sequence

	if err := sqliteStore.WriteInteraction(context.Background(), badInteraction); err == nil {
		t.Fatal("WriteInteraction() expected duplicate-event failure")
	}

	interactions, err := sqliteStore.ListInteractions(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("ListInteractions() error = %v", err)
	}

	if len(interactions) != 0 {
		t.Fatalf("len(ListInteractions()) = %d, want 0 after rolled-back write", len(interactions))
	}
}

func TestStoreRejectsInvalidInteractionBeforePersist(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	badInteraction := validInteraction(run.RunID)
	badInteraction.Service = ""

	if err := sqliteStore.WriteInteraction(context.Background(), badInteraction); err == nil {
		t.Fatal("WriteInteraction() expected validation failure")
	}

	interactions, err := sqliteStore.ListInteractions(context.Background(), run.RunID)
	if err != nil {
		t.Fatalf("ListInteractions() error = %v", err)
	}

	if len(interactions) != 0 {
		t.Fatalf("len(ListInteractions()) = %d, want 0 after rejected write", len(interactions))
	}
}

func TestStorePersistsScrubSalt(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	salt := store.ScrubSalt{
		SessionName:   "onboarding-flow",
		SaltID:        "salt_001",
		SaltEncrypted: []byte("encrypted-salt"),
		CreatedAt:     time.Date(2026, time.April, 21, 15, 0, 0, 0, time.UTC),
	}

	if err := sqliteStore.PutScrubSalt(context.Background(), salt); err != nil {
		t.Fatalf("PutScrubSalt() error = %v", err)
	}

	gotSalt, err := sqliteStore.GetScrubSalt(context.Background(), salt.SessionName)
	if err != nil {
		t.Fatalf("GetScrubSalt() error = %v", err)
	}

	if gotSalt.SaltID != salt.SaltID {
		t.Fatalf("SaltID = %q, want %q", gotSalt.SaltID, salt.SaltID)
	}

	if string(gotSalt.SaltEncrypted) != string(salt.SaltEncrypted) {
		t.Fatalf("SaltEncrypted = %q, want %q", string(gotSalt.SaltEncrypted), string(salt.SaltEncrypted))
	}
}

func TestStoreWritesAndLooksUpBaselines(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	first := store.Baseline{
		BaselineID:  "base_001",
		SessionName: run.SessionName,
		SourceRunID: run.RunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Date(2026, time.April, 21, 15, 0, 0, 0, time.UTC),
	}
	second := store.Baseline{
		BaselineID:  "base_002",
		SessionName: run.SessionName,
		SourceRunID: run.RunID,
		GitSHA:      "def456",
		CreatedAt:   time.Date(2026, time.April, 21, 16, 0, 0, 0, time.UTC),
	}

	for _, baseline := range []store.Baseline{first, second} {
		if err := sqliteStore.PutBaseline(context.Background(), baseline); err != nil {
			t.Fatalf("PutBaseline(%q) error = %v", baseline.BaselineID, err)
		}
	}

	gotBaseline, err := sqliteStore.GetBaseline(context.Background(), first.BaselineID)
	if err != nil {
		t.Fatalf("GetBaseline() error = %v", err)
	}

	if gotBaseline.BaselineID != first.BaselineID {
		t.Fatalf("BaselineID = %q, want %q", gotBaseline.BaselineID, first.BaselineID)
	}

	latest, err := sqliteStore.GetLatestBaseline(context.Background(), run.SessionName)
	if err != nil {
		t.Fatalf("GetLatestBaseline() error = %v", err)
	}

	if latest.BaselineID != second.BaselineID {
		t.Fatalf("LatestBaseline.BaselineID = %q, want %q", latest.BaselineID, second.BaselineID)
	}
}

func TestStoreGetsLatestRunBySession(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	first := validRunRecord()
	second := validRunRecord()
	second.RunID = "run_store_002"
	second.StartedAt = first.StartedAt.Add(10 * time.Minute)
	endedAt := second.StartedAt.Add(250 * time.Millisecond)
	second.EndedAt = &endedAt

	if err := sqliteStore.CreateRun(context.Background(), first); err != nil {
		t.Fatalf("CreateRun(first) error = %v", err)
	}
	if err := sqliteStore.CreateRun(context.Background(), second); err != nil {
		t.Fatalf("CreateRun(second) error = %v", err)
	}

	latest, err := sqliteStore.GetLatestRun(context.Background(), first.SessionName)
	if err != nil {
		t.Fatalf("GetLatestRun() error = %v", err)
	}

	if latest.RunID != second.RunID {
		t.Fatalf("GetLatestRun().RunID = %q, want %q", latest.RunID, second.RunID)
	}
}

func TestStoreGetLatestRunIgnoresNonCompleteLatestRuns(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	complete := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), complete); err != nil {
		t.Fatalf("CreateRun(complete) error = %v", err)
	}

	corrupted := validRunRecord()
	corrupted.RunID = "run_store_corrupted_latest"
	corrupted.Status = store.RunLifecycleStatusCorrupted
	corrupted.StartedAt = complete.StartedAt.Add(10 * time.Minute)
	corruptedEndedAt := corrupted.StartedAt.Add(250 * time.Millisecond)
	corrupted.EndedAt = &corruptedEndedAt
	corrupted.IntegrityIssues = []recorder.IntegrityIssue{
		{
			Code:    recorder.IntegrityIssueRecorderShutdown,
			Message: "simulated failure",
		},
	}
	if err := sqliteStore.CreateRun(context.Background(), corrupted); err != nil {
		t.Fatalf("CreateRun(corrupted) error = %v", err)
	}

	latest, err := sqliteStore.GetLatestRun(context.Background(), complete.SessionName)
	if err != nil {
		t.Fatalf("GetLatestRun() error = %v", err)
	}

	if latest.RunID != complete.RunID {
		t.Fatalf("GetLatestRun().RunID = %q, want replayable run %q", latest.RunID, complete.RunID)
	}
}

func TestStoreGetsLatestRunRecordBySessionAcrossStatuses(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	older := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), older); err != nil {
		t.Fatalf("CreateRun(older) error = %v", err)
	}

	latest := validRunRecord()
	latest.RunID = "run_store_latest_record_001"
	latest.Status = store.RunLifecycleStatusCorrupted
	latest.StartedAt = older.StartedAt.Add(10 * time.Minute)
	endedAt := latest.StartedAt.Add(250 * time.Millisecond)
	latest.EndedAt = &endedAt
	latest.IntegrityIssues = []recorder.IntegrityIssue{
		{
			Code:    recorder.IntegrityIssueRecorderShutdown,
			Message: "simulated failure",
		},
	}
	if err := sqliteStore.CreateRun(context.Background(), latest); err != nil {
		t.Fatalf("CreateRun(latest) error = %v", err)
	}

	gotRecord, err := sqliteStore.GetLatestRunRecord(context.Background(), older.SessionName)
	if err != nil {
		t.Fatalf("GetLatestRunRecord() error = %v", err)
	}

	if gotRecord.RunID != latest.RunID {
		t.Fatalf("GetLatestRunRecord().RunID = %q, want latest run record %q", gotRecord.RunID, latest.RunID)
	}
	if gotRecord.Status != store.RunLifecycleStatusCorrupted {
		t.Fatalf("GetLatestRunRecord().Status = %q, want %q", gotRecord.Status, store.RunLifecycleStatusCorrupted)
	}
}

func TestStoreRejectsBaselineForRunningRun(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunningRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	baseline := store.Baseline{
		BaselineID:  "base_running_001",
		SessionName: run.SessionName,
		SourceRunID: run.RunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Date(2026, time.April, 21, 15, 0, 0, 0, time.UTC),
	}

	if err := sqliteStore.PutBaseline(context.Background(), baseline); err == nil {
		t.Fatal("PutBaseline() expected rejection for running run")
	}
}

func TestStoreRejectsBaselineWithMismatchedSession(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	baseline := store.Baseline{
		BaselineID:  "base_mismatch_001",
		SessionName: "different-session",
		SourceRunID: run.RunID,
		GitSHA:      "abc123",
		CreatedAt:   time.Date(2026, time.April, 21, 15, 0, 0, 0, time.UTC),
	}

	if err := sqliteStore.PutBaseline(context.Background(), baseline); err == nil {
		t.Fatal("PutBaseline() expected rejection for mismatched session")
	}
}

func TestStoreMapsNotFound(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	if _, err := sqliteStore.GetScrubSalt(context.Background(), "missing-session"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetScrubSalt() error = %v, want store.ErrNotFound", err)
	}
}

func TestDeleteSessionRemovesBaselinesViaRunCascade(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	run.RunID = "run_store_delete_session_cascade_001"
	run.SessionName = "cascade-session"
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	baseline := store.Baseline{
		BaselineID:  "base_delete_session_cascade_001",
		SessionName: run.SessionName,
		SourceRunID: run.RunID,
		GitSHA:      "abc123",
		CreatedAt:   run.StartedAt.Add(1 * time.Minute),
	}
	if err := sqliteStore.PutBaseline(context.Background(), baseline); err != nil {
		t.Fatalf("PutBaseline() error = %v", err)
	}

	if err := sqliteStore.DeleteSession(context.Background(), run.SessionName); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if _, err := sqliteStore.GetBaseline(context.Background(), baseline.BaselineID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetBaseline() after DeleteSession error = %v, want store.ErrNotFound", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	sqliteStore, err := OpenStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	return sqliteStore
}

func validRunRecord() store.RunRecord {
	startedAt := time.Date(2026, time.April, 21, 10, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(402 * time.Millisecond)

	return store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_store_001",
		SessionName:        "onboarding-flow",
		Mode:               recorder.RunModeRecord,
		Status:             store.RunLifecycleStatusComplete,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
		EndedAt:            &endedAt,
	}
}

func validRunningRunRecord() store.RunRecord {
	startedAt := time.Date(2026, time.April, 21, 10, 0, 0, 0, time.UTC)

	return store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_store_running_001",
		SessionName:        "onboarding-flow",
		Mode:               recorder.RunModeRecord,
		Status:             store.RunLifecycleStatusRunning,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
	}
}

func validInteraction(runID string) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: "int_store_001",
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Streaming:     true,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions",
			Method: "POST",
			Headers: map[string][]string{
				"content-type": {"application/json"},
			},
			Body: map[string]any{
				"model": "gpt-5.4",
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
				TMS:      234,
				SimTMS:   234,
				Type:     recorder.EventTypeStreamChunk,
				Data: map[string]any{
					"delta": "Hello",
				},
			},
			{
				Sequence: 3,
				TMS:      402,
				SimTMS:   402,
				Type:     recorder.EventTypeStreamEnd,
				Data: map[string]any{
					"finish_reason": "stop",
				},
			},
		},
		ExtractedEntities: []recorder.ExtractedEntity{
			{
				Type: "assistant_message",
				ID:   "msg_store_001",
			},
		},
		ScrubReport: recorder.ScrubReport{
			RedactedPaths:      []string{"request.headers.authorization"},
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_store_001",
		},
		LatencyMS: 402,
	}
}

func TestMigrationCountMatchesEmbeddedMigrations(t *testing.T) {
	t.Parallel()

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	versions := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		versions = append(versions, migration.Version)
	}

	if !slices.Equal(versions, []string{"0001_initial", "0002_add_run_integrity_issues"}) {
		t.Fatalf("migration versions = %v", versions)
	}
}
