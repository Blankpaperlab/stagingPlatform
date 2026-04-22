package store

import (
	"testing"
	"time"

	"stagehand/internal/recorder"
)

func TestRunRecordValidateAllowsRunning(t *testing.T) {
	t.Parallel()

	run := validRunningRunRecord()
	if err := run.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRunRecordToRunRejectsRunning(t *testing.T) {
	t.Parallel()

	run := validRunningRunRecord()
	if _, err := run.ToRun(nil); err == nil {
		t.Fatal("ToRun() expected error for running lifecycle status")
	}
}

func TestRunRecordToRunMapsTerminalStatus(t *testing.T) {
	t.Parallel()

	runRecord := validCompleteRunRecord()
	run, err := runRecord.ToRun(nil)
	if err != nil {
		t.Fatalf("ToRun() error = %v", err)
	}

	if run.Status != recorder.RunStatusComplete {
		t.Fatalf("ToRun().Status = %q, want %q", run.Status, recorder.RunStatusComplete)
	}
}

func validRunningRunRecord() RunRecord {
	startedAt := time.Date(2026, time.April, 21, 10, 0, 0, 0, time.UTC)

	return RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_store_running_unit_001",
		SessionName:        "onboarding-flow",
		Mode:               recorder.RunModeRecord,
		Status:             RunLifecycleStatusRunning,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
	}
}

func validCompleteRunRecord() RunRecord {
	startedAt := time.Date(2026, time.April, 21, 10, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(402 * time.Millisecond)

	return RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_store_complete_unit_001",
		SessionName:        "onboarding-flow",
		Mode:               recorder.RunModeRecord,
		Status:             RunLifecycleStatusComplete,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
		EndedAt:            &endedAt,
	}
}
