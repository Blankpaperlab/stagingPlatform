package contracttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"stagehand/internal/recorder"
	"stagehand/internal/store"
)

type Factory func(t *testing.T) store.ArtifactStore

func RunArtifactStoreTests(t *testing.T, newStore Factory) {
	t.Helper()

	t.Run("run lifecycle transitions", func(t *testing.T) {
		t.Parallel()

		artifactStore := newStore(t)
		defer artifactStore.Close()

		run := newRunRecord("run_contract_transition_001", "session_contract_transition", store.RunLifecycleStatusRunning)
		if err := artifactStore.CreateRun(context.Background(), run); err != nil {
			t.Fatalf("CreateRun() error = %v", err)
		}

		gotRecord, err := artifactStore.GetRunRecord(context.Background(), run.RunID)
		if err != nil {
			t.Fatalf("GetRunRecord() error = %v", err)
		}

		if gotRecord.Status != store.RunLifecycleStatusRunning {
			t.Fatalf("GetRunRecord().Status = %q, want %q", gotRecord.Status, store.RunLifecycleStatusRunning)
		}

		if _, err := artifactStore.GetRun(context.Background(), run.RunID); err == nil {
			t.Fatal("GetRun() expected failure for running run")
		}

		interaction := newInteraction(run.RunID, "int_contract_transition_001", 1)
		if err := artifactStore.WriteInteraction(context.Background(), interaction); err != nil {
			t.Fatalf("WriteInteraction() error = %v", err)
		}

		completed := run
		completed.Status = store.RunLifecycleStatusComplete
		endedAt := run.StartedAt.Add(2 * time.Second)
		completed.EndedAt = &endedAt
		if err := artifactStore.UpdateRun(context.Background(), completed); err != nil {
			t.Fatalf("UpdateRun() error = %v", err)
		}

		gotRun, err := artifactStore.GetRun(context.Background(), run.RunID)
		if err != nil {
			t.Fatalf("GetRun() after completion error = %v", err)
		}

		if gotRun.Status != recorder.RunStatusComplete {
			t.Fatalf("GetRun().Status = %q, want %q", gotRun.Status, recorder.RunStatusComplete)
		}

		if len(gotRun.Interactions) != 1 {
			t.Fatalf("len(GetRun().Interactions) = %d, want 1", len(gotRun.Interactions))
		}
	})

	t.Run("interaction ordering", func(t *testing.T) {
		t.Parallel()

		artifactStore := newStore(t)
		defer artifactStore.Close()

		run := newRunRecord("run_contract_order_001", "session_contract_order", store.RunLifecycleStatusRunning)
		if err := artifactStore.CreateRun(context.Background(), run); err != nil {
			t.Fatalf("CreateRun() error = %v", err)
		}

		second := newInteraction(run.RunID, "int_contract_order_002", 2)
		first := newInteraction(run.RunID, "int_contract_order_001", 1)

		if err := artifactStore.WriteInteraction(context.Background(), second); err != nil {
			t.Fatalf("WriteInteraction(second) error = %v", err)
		}
		if err := artifactStore.WriteInteraction(context.Background(), first); err != nil {
			t.Fatalf("WriteInteraction(first) error = %v", err)
		}

		interactions, err := artifactStore.ListInteractions(context.Background(), run.RunID)
		if err != nil {
			t.Fatalf("ListInteractions() error = %v", err)
		}

		if len(interactions) != 2 {
			t.Fatalf("len(ListInteractions()) = %d, want 2", len(interactions))
		}

		if interactions[0].Sequence != 1 || interactions[1].Sequence != 2 {
			t.Fatalf("interaction sequence order = [%d, %d], want [1, 2]", interactions[0].Sequence, interactions[1].Sequence)
		}

		completed := run
		completed.Status = store.RunLifecycleStatusComplete
		endedAt := run.StartedAt.Add(3 * time.Second)
		completed.EndedAt = &endedAt
		if err := artifactStore.UpdateRun(context.Background(), completed); err != nil {
			t.Fatalf("UpdateRun() error = %v", err)
		}

		gotRun, err := artifactStore.GetRun(context.Background(), run.RunID)
		if err != nil {
			t.Fatalf("GetRun() error = %v", err)
		}

		if len(gotRun.Interactions) != 2 {
			t.Fatalf("len(GetRun().Interactions) = %d, want 2", len(gotRun.Interactions))
		}

		if gotRun.Interactions[0].Sequence != 1 || gotRun.Interactions[1].Sequence != 2 {
			t.Fatalf("hydrated interaction sequence order = [%d, %d], want [1, 2]", gotRun.Interactions[0].Sequence, gotRun.Interactions[1].Sequence)
		}
	})

	t.Run("corrupted and incomplete run handling", func(t *testing.T) {
		t.Parallel()

		artifactStore := newStore(t)
		defer artifactStore.Close()

		incomplete := newTerminalRunRecord("run_contract_incomplete_001", "session_contract_incomplete", store.RunLifecycleStatusIncomplete)
		if err := artifactStore.CreateRun(context.Background(), incomplete); err != nil {
			t.Fatalf("CreateRun(incomplete) error = %v", err)
		}

		gotIncomplete, err := artifactStore.GetRun(context.Background(), incomplete.RunID)
		if err != nil {
			t.Fatalf("GetRun(incomplete) error = %v", err)
		}

		if gotIncomplete.Status != recorder.RunStatusIncomplete {
			t.Fatalf("GetRun(incomplete).Status = %q, want %q", gotIncomplete.Status, recorder.RunStatusIncomplete)
		}

		if gotIncomplete.ReplayEligible() {
			t.Fatal("GetRun(incomplete).ReplayEligible() = true, want false")
		}

		incompleteBaseline := store.Baseline{
			BaselineID:  "base_contract_incomplete_001",
			SessionName: incomplete.SessionName,
			SourceRunID: incomplete.RunID,
			GitSHA:      "abc123",
			CreatedAt:   incomplete.StartedAt.Add(1 * time.Minute),
		}
		if err := artifactStore.PutBaseline(context.Background(), incompleteBaseline); err == nil {
			t.Fatal("PutBaseline(incomplete) expected failure")
		}

		mismatchedSessionBaseline := store.Baseline{
			BaselineID:  "base_contract_mismatch_001",
			SessionName: "different-session",
			SourceRunID: incomplete.RunID,
			GitSHA:      "abc123",
			CreatedAt:   incomplete.StartedAt.Add(2 * time.Minute),
		}
		if err := artifactStore.PutBaseline(context.Background(), mismatchedSessionBaseline); err == nil {
			t.Fatal("PutBaseline(mismatched session) expected failure")
		}

		corrupted := newTerminalRunRecord("run_contract_corrupted_001", "session_contract_corrupted", store.RunLifecycleStatusCorrupted)
		if err := artifactStore.CreateRun(context.Background(), corrupted); err != nil {
			t.Fatalf("CreateRun(corrupted) error = %v", err)
		}

		gotCorrupted, err := artifactStore.GetRun(context.Background(), corrupted.RunID)
		if err != nil {
			t.Fatalf("GetRun(corrupted) error = %v", err)
		}

		if gotCorrupted.Status != recorder.RunStatusCorrupted {
			t.Fatalf("GetRun(corrupted).Status = %q, want %q", gotCorrupted.Status, recorder.RunStatusCorrupted)
		}

		if len(gotCorrupted.IntegrityIssues) == 0 {
			t.Fatal("GetRun(corrupted).IntegrityIssues should not be empty")
		}
	})

	t.Run("deletion behavior", func(t *testing.T) {
		t.Parallel()

		artifactStore := newStore(t)
		defer artifactStore.Close()

		sessionName := "session_contract_delete"
		runKeep := newTerminalRunRecord("run_contract_delete_keep_001", sessionName, store.RunLifecycleStatusComplete)
		runDelete := newTerminalRunRecord("run_contract_delete_target_001", sessionName, store.RunLifecycleStatusComplete)

		for _, run := range []store.RunRecord{runKeep, runDelete} {
			if err := artifactStore.CreateRun(context.Background(), run); err != nil {
				t.Fatalf("CreateRun(%q) error = %v", run.RunID, err)
			}
		}

		if err := artifactStore.WriteInteraction(context.Background(), newInteraction(runDelete.RunID, "int_contract_delete_001", 1)); err != nil {
			t.Fatalf("WriteInteraction() error = %v", err)
		}

		salt := store.ScrubSalt{
			SessionName:   sessionName,
			SaltID:        "salt_contract_delete_001",
			SaltEncrypted: []byte("encrypted-salt"),
			CreatedAt:     runDelete.StartedAt,
		}
		if err := artifactStore.PutScrubSalt(context.Background(), salt); err != nil {
			t.Fatalf("PutScrubSalt() error = %v", err)
		}

		baseline := store.Baseline{
			BaselineID:  "base_contract_delete_001",
			SessionName: sessionName,
			SourceRunID: runDelete.RunID,
			GitSHA:      "abc123",
			CreatedAt:   runDelete.StartedAt.Add(1 * time.Minute),
		}
		if err := artifactStore.PutBaseline(context.Background(), baseline); err != nil {
			t.Fatalf("PutBaseline() error = %v", err)
		}

		if err := artifactStore.DeleteRun(context.Background(), runDelete.RunID); err != nil {
			t.Fatalf("DeleteRun() error = %v", err)
		}

		if _, err := artifactStore.GetRunRecord(context.Background(), runDelete.RunID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetRunRecord(deleted run) error = %v, want store.ErrNotFound", err)
		}

		if _, err := artifactStore.GetBaseline(context.Background(), baseline.BaselineID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetBaseline(deleted run baseline) error = %v, want store.ErrNotFound", err)
		}

		if _, err := artifactStore.GetScrubSalt(context.Background(), sessionName); err != nil {
			t.Fatalf("GetScrubSalt() after DeleteRun error = %v", err)
		}

		if _, err := artifactStore.GetRunRecord(context.Background(), runKeep.RunID); err != nil {
			t.Fatalf("GetRunRecord(remaining run) error = %v", err)
		}

		if err := artifactStore.DeleteSession(context.Background(), sessionName); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}

		if _, err := artifactStore.GetRunRecord(context.Background(), runKeep.RunID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetRunRecord(after DeleteSession) error = %v, want store.ErrNotFound", err)
		}

		if _, err := artifactStore.GetScrubSalt(context.Background(), sessionName); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetScrubSalt(after DeleteSession) error = %v, want store.ErrNotFound", err)
		}

		if _, err := artifactStore.GetLatestBaseline(context.Background(), sessionName); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetLatestBaseline(after DeleteSession) error = %v, want store.ErrNotFound", err)
		}

		if err := artifactStore.DeleteSession(context.Background(), sessionName); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("DeleteSession(missing) error = %v, want store.ErrNotFound", err)
		}
	})
}

func newRunRecord(runID, sessionName string, status store.RunLifecycleStatus) store.RunRecord {
	startedAt := time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)

	return store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              runID,
		SessionName:        sessionName,
		Mode:               recorder.RunModeRecord,
		Status:             status,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
	}
}

func newTerminalRunRecord(runID, sessionName string, status store.RunLifecycleStatus) store.RunRecord {
	run := newRunRecord(runID, sessionName, status)
	if status == store.RunLifecycleStatusComplete {
		endedAt := run.StartedAt.Add(2 * time.Second)
		run.EndedAt = &endedAt
		return run
	}

	run.IntegrityIssues = []recorder.IntegrityIssue{
		{
			Code:    recorder.IntegrityIssueInterruptedWrite,
			Message: fmt.Sprintf("run %s ended in state %s", runID, status),
		},
	}

	return run
}

func newInteraction(runID, interactionID string, sequence int) recorder.Interaction {
	baseMS := int64(sequence * 100)

	return recorder.Interaction{
		RunID:         runID,
		InteractionID: interactionID,
		Sequence:      sequence,
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
				TMS:      baseMS,
				SimTMS:   baseMS,
				Type:     recorder.EventTypeRequestSent,
			},
			{
				Sequence: 2,
				TMS:      baseMS + 10,
				SimTMS:   baseMS + 10,
				Type:     recorder.EventTypeStreamEnd,
				Data: map[string]any{
					"finish_reason": "stop",
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			RedactedPaths:      []string{"request.headers.authorization"},
			ScrubPolicyVersion: "v1",
			SessionSaltID:      "salt_contract_001",
		},
		LatencyMS: 10,
	}
}
