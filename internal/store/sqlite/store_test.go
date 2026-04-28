package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"stagehand/internal/recorder"
	"stagehand/internal/store"
)

type rowsAffectedErrorResult struct{}

func (rowsAffectedErrorResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (rowsAffectedErrorResult) RowsAffected() (int64, error) {
	return 0, driver.ErrBadConn
}

func TestInspectRowsAffectedReturnsDriverErrors(t *testing.T) {
	t.Parallel()

	_, err := inspectRowsAffected(rowsAffectedErrorResult{}, "test operation")
	if err == nil {
		t.Fatal("inspectRowsAffected() expected error")
	}
	if !strings.Contains(err.Error(), "test operation rows affected") {
		t.Fatalf("inspectRowsAffected() error = %q, want operation context", err)
	}
}

func TestStoreCreateGetAndUpdateRun(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	run.Metadata = map[string]any{
		"error_injection": map[string]any{
			"applied": []any{
				map[string]any{
					"rule_index": float64(0),
					"service":    "stripe",
				},
			},
		},
	}
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
	if gotRun.Metadata["error_injection"] == nil {
		t.Fatalf("GetRun().Metadata = %#v, want error_injection metadata", gotRun.Metadata)
	}

	updated := run
	updated.AgentVersion = "agent-v2"
	updated.GitSHA = "def456"
	updated.Metadata = map[string]any{
		"error_injection": map[string]any{
			"applied": []any{
				map[string]any{
					"rule_index": float64(1),
					"service":    "openai",
				},
			},
		},
	}

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
	gotMetadata := gotUpdatedRun.Metadata["error_injection"].(map[string]any)
	gotApplied := gotMetadata["applied"].([]any)
	gotAppliedEntry := gotApplied[0].(map[string]any)
	if gotAppliedEntry["service"] != "openai" {
		t.Fatalf("GetRun().Metadata service = %q, want openai", gotAppliedEntry["service"])
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

func TestStorePersistsInteractionFallbackTier(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	run := validRunRecord()
	if err := sqliteStore.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	interaction := validInteraction(run.RunID)
	interaction.FallbackTier = recorder.FallbackTierNearestNeighbor
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
	if interactions[0].FallbackTier != recorder.FallbackTierNearestNeighbor {
		t.Fatalf("FallbackTier = %q, want %q", interactions[0].FallbackTier, recorder.FallbackTierNearestNeighbor)
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

func TestStoreCreateScrubSaltIfAbsentReturnsFirstWriter(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	first := store.ScrubSalt{
		SessionName:   "onboarding-flow",
		SaltID:        "salt_first",
		SaltEncrypted: []byte("encrypted-first"),
		CreatedAt:     time.Date(2026, time.April, 23, 12, 0, 0, 0, time.UTC),
	}

	persistedFirst, created, err := sqliteStore.CreateScrubSaltIfAbsent(context.Background(), first)
	if err != nil {
		t.Fatalf("CreateScrubSaltIfAbsent(first) error = %v", err)
	}
	if !created {
		t.Fatal("CreateScrubSaltIfAbsent(first) = created false, want true")
	}
	if persistedFirst.SaltID != first.SaltID {
		t.Fatalf("persistedFirst.SaltID = %q, want %q", persistedFirst.SaltID, first.SaltID)
	}

	second := store.ScrubSalt{
		SessionName:   first.SessionName,
		SaltID:        "salt_second",
		SaltEncrypted: []byte("encrypted-second"),
		CreatedAt:     time.Date(2026, time.April, 23, 12, 1, 0, 0, time.UTC),
	}

	persistedSecond, created, err := sqliteStore.CreateScrubSaltIfAbsent(context.Background(), second)
	if err != nil {
		t.Fatalf("CreateScrubSaltIfAbsent(second) error = %v", err)
	}
	if created {
		t.Fatal("CreateScrubSaltIfAbsent(second) = created true, want false")
	}
	if persistedSecond.SaltID != first.SaltID {
		t.Fatalf("persistedSecond.SaltID = %q, want first-writer salt %q", persistedSecond.SaltID, first.SaltID)
	}
	if string(persistedSecond.SaltEncrypted) != string(first.SaltEncrypted) {
		t.Fatalf(
			"persistedSecond.SaltEncrypted = %q, want first-writer payload %q",
			string(persistedSecond.SaltEncrypted),
			string(first.SaltEncrypted),
		)
	}

	gotSalt, err := sqliteStore.GetScrubSalt(context.Background(), first.SessionName)
	if err != nil {
		t.Fatalf("GetScrubSalt() error = %v", err)
	}
	if gotSalt.SaltID != first.SaltID {
		t.Fatalf("GetScrubSalt().SaltID = %q, want %q", gotSalt.SaltID, first.SaltID)
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

func TestStoreSessionLifecycleStateAndSnapshots(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	createdAt := time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)
	session := store.SessionRecord{
		SessionName: "runtime-session",
		Status:      store.SessionStatusActive,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	snapshot := store.SessionSnapshot{
		SnapshotID:  "snap_runtime_001",
		SessionName: session.SessionName,
		State: map[string]any{
			"stripe": map[string]any{
				"customers": []any{"cus_001"},
			},
		},
		CreatedAt: createdAt.Add(time.Second),
	}
	if err := sqliteStore.PutSessionSnapshot(context.Background(), snapshot); err != nil {
		t.Fatalf("PutSessionSnapshot() error = %v", err)
	}

	session.CurrentSnapshotID = snapshot.SnapshotID
	session.UpdatedAt = snapshot.CreatedAt
	if err := sqliteStore.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}

	gotSession, err := sqliteStore.GetSession(context.Background(), session.SessionName)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.CurrentSnapshotID != snapshot.SnapshotID {
		t.Fatalf("CurrentSnapshotID = %q, want %q", gotSession.CurrentSnapshotID, snapshot.SnapshotID)
	}

	gotSnapshot, err := sqliteStore.GetLatestSessionSnapshot(context.Background(), session.SessionName)
	if err != nil {
		t.Fatalf("GetLatestSessionSnapshot() error = %v", err)
	}
	if gotSnapshot.SnapshotID != snapshot.SnapshotID {
		t.Fatalf("SnapshotID = %q, want %q", gotSnapshot.SnapshotID, snapshot.SnapshotID)
	}
	if gotSnapshot.State["stripe"] == nil {
		t.Fatalf("snapshot state = %#v, want stripe state", gotSnapshot.State)
	}
}

func TestStoreAppendSessionSnapshotChainsConcurrentWriters(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	ctx := context.Background()
	base := time.Date(2026, time.April, 24, 11, 0, 0, 0, time.UTC)
	session := store.SessionRecord{
		SessionName: "runtime-session-concurrent",
		Status:      store.SessionStatusActive,
		CreatedAt:   base,
		UpdatedAt:   base,
	}
	if err := sqliteStore.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	const concurrent = 16
	var wg sync.WaitGroup
	errs := make(chan error, concurrent)
	ids := make(chan string, concurrent)
	for idx := 0; idx < concurrent; idx++ {
		idx := idx
		wg.Add(1)
		go func() {
			defer wg.Done()

			snapshotID := fmt.Sprintf("snap_runtime_%03d", idx)
			createdAt := base.Add(time.Duration(idx+1) * time.Second)
			snapshot, err := sqliteStore.AppendSessionSnapshot(ctx, store.SessionSnapshot{
				SnapshotID:  snapshotID,
				SessionName: session.SessionName,
				State: map[string]any{
					"counter": idx,
				},
				CreatedAt: createdAt,
			}, createdAt)
			if err != nil {
				errs <- err
				return
			}
			ids <- snapshot.SnapshotID
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)

	for err := range errs {
		t.Fatalf("AppendSessionSnapshot() concurrent error = %v", err)
	}

	expected := map[string]bool{}
	for snapshotID := range ids {
		expected[snapshotID] = true
	}
	if len(expected) != concurrent {
		t.Fatalf("unique snapshot ids = %d, want %d", len(expected), concurrent)
	}

	gotSession, err := sqliteStore.GetSession(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if gotSession.CurrentSnapshotID == "" {
		t.Fatal("CurrentSnapshotID = empty, want latest appended snapshot")
	}

	visited := map[string]bool{}
	cursor := gotSession.CurrentSnapshotID
	for cursor != "" {
		if visited[cursor] {
			t.Fatalf("snapshot cycle at %q", cursor)
		}
		visited[cursor] = true

		snapshot, err := sqliteStore.GetSessionSnapshot(ctx, cursor)
		if err != nil {
			t.Fatalf("GetSessionSnapshot(%q) error = %v", cursor, err)
		}
		cursor = snapshot.ParentSnapshotID
	}

	if len(visited) != concurrent {
		t.Fatalf("reachable lineage size = %d, want %d", len(visited), concurrent)
	}
	for snapshotID := range expected {
		if !visited[snapshotID] {
			t.Fatalf("snapshot %q was orphaned from the current lineage", snapshotID)
		}
	}
}

func TestStorePersistsSessionClockAndScheduledEvents(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	ctx := context.Background()
	base := time.Date(2026, time.April, 24, 13, 0, 0, 0, time.UTC)
	session := store.SessionRecord{
		SessionName: "queue-session",
		Status:      store.SessionStatusActive,
		CreatedAt:   base,
		UpdatedAt:   base,
	}
	if err := sqliteStore.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	clock := store.SessionClock{
		SessionName: session.SessionName,
		CurrentTime: base,
		UpdatedAt:   base,
	}
	if err := sqliteStore.PutSessionClock(ctx, clock); err != nil {
		t.Fatalf("PutSessionClock() error = %v", err)
	}

	gotClock, err := sqliteStore.GetSessionClock(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("GetSessionClock() error = %v", err)
	}
	if !gotClock.CurrentTime.Equal(clock.CurrentTime) {
		t.Fatalf("CurrentTime = %s, want %s", gotClock.CurrentTime, clock.CurrentTime)
	}

	pushDue := scheduledEvent("evt_push_due", session.SessionName, store.ScheduledEventDeliveryModePush, base.Add(5*time.Minute))
	pullDue := scheduledEvent("evt_pull_due", session.SessionName, store.ScheduledEventDeliveryModePull, base.Add(5*time.Minute))
	pushLater := scheduledEvent("evt_push_later", session.SessionName, store.ScheduledEventDeliveryModePush, base.Add(10*time.Minute))

	for _, event := range []store.ScheduledEvent{pushLater, pullDue, pushDue} {
		if err := sqliteStore.PutScheduledEvent(ctx, event); err != nil {
			t.Fatalf("PutScheduledEvent(%q) error = %v", event.EventID, err)
		}
	}

	duePush, err := sqliteStore.ListDueScheduledEvents(ctx, session.SessionName, store.ScheduledEventDeliveryModePush, base.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ListDueScheduledEvents(push) error = %v", err)
	}
	if len(duePush) != 1 || duePush[0].EventID != pushDue.EventID {
		t.Fatalf("due push events = %#v, want only %q", duePush, pushDue.EventID)
	}

	duePull, err := sqliteStore.ListDueScheduledEvents(ctx, session.SessionName, store.ScheduledEventDeliveryModePull, base.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ListDueScheduledEvents(pull) error = %v", err)
	}
	if len(duePull) != 1 || duePull[0].EventID != pullDue.EventID {
		t.Fatalf("due pull events = %#v, want only %q", duePull, pullDue.EventID)
	}

	deliveredAt := base.Add(5 * time.Minute)
	if err := sqliteStore.MarkScheduledEventsDelivered(ctx, session.SessionName, []string{pushDue.EventID}, deliveredAt); err != nil {
		t.Fatalf("MarkScheduledEventsDelivered() error = %v", err)
	}

	gotEvent, err := sqliteStore.GetScheduledEvent(ctx, pushDue.EventID)
	if err != nil {
		t.Fatalf("GetScheduledEvent() error = %v", err)
	}
	if gotEvent.Status != store.ScheduledEventStatusDelivered {
		t.Fatalf("Status = %q, want %q", gotEvent.Status, store.ScheduledEventStatusDelivered)
	}
	if gotEvent.DeliveredAt == nil || !gotEvent.DeliveredAt.Equal(deliveredAt) {
		t.Fatalf("DeliveredAt = %v, want %s", gotEvent.DeliveredAt, deliveredAt)
	}

	duePush, err = sqliteStore.ListDueScheduledEvents(ctx, session.SessionName, store.ScheduledEventDeliveryModePush, base.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("ListDueScheduledEvents(after mark) error = %v", err)
	}
	if len(duePush) != 0 {
		t.Fatalf("due push after delivery = %#v, want empty", duePush)
	}
}

func TestStoreEnsuresAndAdvancesSessionClockAtomically(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	ctx := context.Background()
	base := time.Date(2026, time.April, 24, 13, 0, 0, 0, time.UTC)
	session := store.SessionRecord{
		SessionName: "queue-clock-atomic",
		Status:      store.SessionStatusActive,
		CreatedAt:   base,
		UpdatedAt:   base,
	}
	if err := sqliteStore.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	clock := store.SessionClock{
		SessionName: session.SessionName,
		CurrentTime: base,
		UpdatedAt:   base,
	}
	ensured, created, err := sqliteStore.EnsureSessionClock(ctx, clock)
	if err != nil {
		t.Fatalf("EnsureSessionClock(first) error = %v", err)
	}
	if !created {
		t.Fatal("EnsureSessionClock(first) created = false, want true")
	}
	if !ensured.CurrentTime.Equal(base) {
		t.Fatalf("ensured.CurrentTime = %s, want %s", ensured.CurrentTime, base)
	}

	later := base.Add(time.Hour)
	ensuredAgain, createdAgain, err := sqliteStore.EnsureSessionClock(ctx, store.SessionClock{
		SessionName: session.SessionName,
		CurrentTime: later,
		UpdatedAt:   later,
	})
	if err != nil {
		t.Fatalf("EnsureSessionClock(second) error = %v", err)
	}
	if createdAgain {
		t.Fatal("EnsureSessionClock(second) created = true, want false")
	}
	if !ensuredAgain.CurrentTime.Equal(base) {
		t.Fatalf("ensuredAgain.CurrentTime = %s, want original %s", ensuredAgain.CurrentTime, base)
	}

	advanced, err := sqliteStore.AdvanceSessionClock(ctx, session.SessionName, 5*time.Second, later, later)
	if err != nil {
		t.Fatalf("AdvanceSessionClock() error = %v", err)
	}
	if !advanced.CurrentTime.Equal(base.Add(5 * time.Second)) {
		t.Fatalf("advanced.CurrentTime = %s, want %s", advanced.CurrentTime, base.Add(5*time.Second))
	}
}

func TestDeleteSessionRemovesRuntimeSessionStateOnlyForTarget(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	createdAt := time.Date(2026, time.April, 24, 10, 0, 0, 0, time.UTC)
	for _, sessionName := range []string{"delete-target", "keep-target"} {
		session := store.SessionRecord{
			SessionName: sessionName,
			Status:      store.SessionStatusActive,
			CreatedAt:   createdAt,
			UpdatedAt:   createdAt,
		}
		if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
			t.Fatalf("CreateSession(%q) error = %v", sessionName, err)
		}
		if err := sqliteStore.PutSessionSnapshot(context.Background(), store.SessionSnapshot{
			SnapshotID:  "snap_" + sessionName,
			SessionName: sessionName,
			State:       map[string]any{"value": sessionName},
			CreatedAt:   createdAt.Add(time.Second),
		}); err != nil {
			t.Fatalf("PutSessionSnapshot(%q) error = %v", sessionName, err)
		}
	}

	if err := sqliteStore.DeleteSession(context.Background(), "delete-target"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if _, err := sqliteStore.GetSession(context.Background(), "delete-target"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSession(deleted) error = %v, want store.ErrNotFound", err)
	}
	if _, err := sqliteStore.GetLatestSessionSnapshot(context.Background(), "delete-target"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetLatestSessionSnapshot(deleted) error = %v, want store.ErrNotFound", err)
	}
	if _, err := sqliteStore.GetSession(context.Background(), "keep-target"); err != nil {
		t.Fatalf("GetSession(kept) error = %v", err)
	}
	if _, err := sqliteStore.GetLatestSessionSnapshot(context.Background(), "keep-target"); err != nil {
		t.Fatalf("GetLatestSessionSnapshot(kept) error = %v", err)
	}
}

func TestDeleteSessionRemovesEventQueueStateOnlyForTarget(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	ctx := context.Background()
	base := time.Date(2026, time.April, 24, 14, 0, 0, 0, time.UTC)
	for _, sessionName := range []string{"queue-delete-target", "queue-keep-target"} {
		session := store.SessionRecord{
			SessionName: sessionName,
			Status:      store.SessionStatusActive,
			CreatedAt:   base,
			UpdatedAt:   base,
		}
		if err := sqliteStore.CreateSession(ctx, session); err != nil {
			t.Fatalf("CreateSession(%q) error = %v", sessionName, err)
		}
		if err := sqliteStore.PutSessionClock(ctx, store.SessionClock{
			SessionName: sessionName,
			CurrentTime: base,
			UpdatedAt:   base,
		}); err != nil {
			t.Fatalf("PutSessionClock(%q) error = %v", sessionName, err)
		}
		event := scheduledEvent("evt_"+sessionName, sessionName, store.ScheduledEventDeliveryModePull, base.Add(time.Minute))
		if err := sqliteStore.PutScheduledEvent(ctx, event); err != nil {
			t.Fatalf("PutScheduledEvent(%q) error = %v", sessionName, err)
		}
	}

	if err := sqliteStore.DeleteSession(ctx, "queue-delete-target"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if _, err := sqliteStore.GetSessionClock(ctx, "queue-delete-target"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSessionClock(deleted) error = %v, want store.ErrNotFound", err)
	}
	if _, err := sqliteStore.GetScheduledEvent(ctx, "evt_queue-delete-target"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetScheduledEvent(deleted) error = %v, want store.ErrNotFound", err)
	}
	if _, err := sqliteStore.GetSessionClock(ctx, "queue-keep-target"); err != nil {
		t.Fatalf("GetSessionClock(kept) error = %v", err)
	}
	if _, err := sqliteStore.GetScheduledEvent(ctx, "evt_queue-keep-target"); err != nil {
		t.Fatalf("GetScheduledEvent(kept) error = %v", err)
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

func scheduledEvent(eventID string, sessionName string, mode store.ScheduledEventDeliveryMode, dueAt time.Time) store.ScheduledEvent {
	return store.ScheduledEvent{
		EventID:      eventID,
		SessionName:  sessionName,
		Service:      "stripe",
		Topic:        "webhook.payment_intent.succeeded",
		DeliveryMode: mode,
		DueAt:        dueAt,
		Payload: map[string]any{
			"object_id": "pi_test_001",
		},
		Status:    store.ScheduledEventStatusPending,
		CreatedAt: dueAt.Add(-time.Minute),
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

	if !slices.Equal(versions, []string{
		"0001_initial",
		"0002_add_run_integrity_issues",
		"0003_session_lifecycle",
		"0004_event_queue",
		"0005_add_run_metadata",
	}) {
		t.Fatalf("migration versions = %v", versions)
	}
}
