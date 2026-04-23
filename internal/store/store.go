package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"stagehand/internal/recorder"
)

var ErrNotFound = errors.New("store record not found")

type RunLifecycleStatus string

const (
	RunLifecycleStatusRunning    RunLifecycleStatus = "running"
	RunLifecycleStatusComplete   RunLifecycleStatus = "complete"
	RunLifecycleStatusIncomplete RunLifecycleStatus = "incomplete"
	RunLifecycleStatusCorrupted  RunLifecycleStatus = "corrupted"
)

var validRunLifecycleStatuses = []RunLifecycleStatus{
	RunLifecycleStatusRunning,
	RunLifecycleStatusComplete,
	RunLifecycleStatusIncomplete,
	RunLifecycleStatusCorrupted,
}

type ArtifactStore interface {
	CreateRun(ctx context.Context, run RunRecord) error
	GetRunRecord(ctx context.Context, runID string) (RunRecord, error)
	GetLatestRunRecord(ctx context.Context, sessionName string) (RunRecord, error)
	GetRun(ctx context.Context, runID string) (recorder.Run, error)
	GetLatestRun(ctx context.Context, sessionName string) (recorder.Run, error)
	UpdateRun(ctx context.Context, run RunRecord) error
	DeleteRun(ctx context.Context, runID string) error
	DeleteSession(ctx context.Context, sessionName string) error
	WriteInteraction(ctx context.Context, interaction recorder.Interaction) error
	ListInteractions(ctx context.Context, runID string) ([]recorder.Interaction, error)
	PutScrubSalt(ctx context.Context, salt ScrubSalt) error
	GetScrubSalt(ctx context.Context, sessionName string) (ScrubSalt, error)
	PutBaseline(ctx context.Context, baseline Baseline) error
	GetBaseline(ctx context.Context, baselineID string) (Baseline, error)
	GetLatestBaseline(ctx context.Context, sessionName string) (Baseline, error)
	Close() error
}

type RunRecord struct {
	SchemaVersion      string
	SDKVersion         string
	RuntimeVersion     string
	ScrubPolicyVersion string
	RunID              string
	SessionName        string
	Mode               recorder.RunMode
	Status             RunLifecycleStatus
	BaseSnapshotID     string
	AgentVersion       string
	GitSHA             string
	StartedAt          time.Time
	EndedAt            *time.Time
	IntegrityIssues    []recorder.IntegrityIssue
}

func RunRecordFromRun(run recorder.Run) RunRecord {
	return RunRecord{
		SchemaVersion:      run.SchemaVersion,
		SDKVersion:         run.SDKVersion,
		RuntimeVersion:     run.RuntimeVersion,
		ScrubPolicyVersion: run.ScrubPolicyVersion,
		RunID:              run.RunID,
		SessionName:        run.SessionName,
		Mode:               run.Mode,
		Status:             RunLifecycleStatus(run.Status),
		BaseSnapshotID:     run.BaseSnapshotID,
		AgentVersion:       run.AgentVersion,
		GitSHA:             run.GitSHA,
		StartedAt:          run.StartedAt,
		EndedAt:            run.EndedAt,
		IntegrityIssues:    run.IntegrityIssues,
	}
}

func (r RunRecord) ToRun(interactions []recorder.Interaction) (recorder.Run, error) {
	artifactStatus, err := r.ArtifactStatus()
	if err != nil {
		return recorder.Run{}, err
	}

	return recorder.Run{
		SchemaVersion:      r.SchemaVersion,
		SDKVersion:         r.SDKVersion,
		RuntimeVersion:     r.RuntimeVersion,
		ScrubPolicyVersion: r.ScrubPolicyVersion,
		RunID:              r.RunID,
		SessionName:        r.SessionName,
		Mode:               r.Mode,
		Status:             artifactStatus,
		BaseSnapshotID:     r.BaseSnapshotID,
		AgentVersion:       r.AgentVersion,
		GitSHA:             r.GitSHA,
		StartedAt:          r.StartedAt,
		EndedAt:            r.EndedAt,
		Interactions:       interactions,
		IntegrityIssues:    r.IntegrityIssues,
	}, nil
}

func (r RunRecord) ArtifactStatus() (recorder.RunStatus, error) {
	switch r.Status {
	case RunLifecycleStatusComplete:
		return recorder.RunStatusComplete, nil
	case RunLifecycleStatusIncomplete:
		return recorder.RunStatusIncomplete, nil
	case RunLifecycleStatusCorrupted:
		return recorder.RunStatusCorrupted, nil
	case RunLifecycleStatusRunning:
		return "", fmt.Errorf("run %q is still %q and cannot be materialized as an artifact", r.RunID, r.Status)
	default:
		return "", fmt.Errorf("run %q has unsupported lifecycle status %q", r.RunID, r.Status)
	}
}

func (r RunRecord) Validate() error {
	var problems []string

	if r.SchemaVersion != recorder.ArtifactSchemaVersion {
		problems = append(problems, fmt.Sprintf("schema_version must be %q", recorder.ArtifactSchemaVersion))
	}

	if strings.TrimSpace(r.SDKVersion) == "" {
		problems = append(problems, "sdk_version is required")
	}

	if strings.TrimSpace(r.RuntimeVersion) == "" {
		problems = append(problems, "runtime_version is required")
	}

	if strings.TrimSpace(r.ScrubPolicyVersion) == "" {
		problems = append(problems, "scrub_policy_version is required")
	}

	if strings.TrimSpace(r.RunID) == "" {
		problems = append(problems, "run_id is required")
	}

	if strings.TrimSpace(r.SessionName) == "" {
		problems = append(problems, "session_name is required")
	}

	if !slices.Contains(validRunLifecycleStatuses, r.Status) {
		problems = append(problems, fmt.Sprintf("status must be one of %q, %q, %q, %q", RunLifecycleStatusRunning, RunLifecycleStatusComplete, RunLifecycleStatusIncomplete, RunLifecycleStatusCorrupted))
	}

	if r.StartedAt.IsZero() {
		problems = append(problems, "started_at is required")
	}

	if r.EndedAt != nil && !r.StartedAt.IsZero() && r.EndedAt.Before(r.StartedAt) {
		problems = append(problems, "ended_at cannot be before started_at")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid run record: %s", strings.Join(problems, "; "))
	}

	if r.Status == RunLifecycleStatusRunning {
		if r.EndedAt != nil && !r.EndedAt.IsZero() {
			return fmt.Errorf("invalid run record: ended_at must be empty when status is %q", RunLifecycleStatusRunning)
		}
		if len(r.IntegrityIssues) > 0 {
			return fmt.Errorf("invalid run record: integrity_issues must be empty when status is %q", RunLifecycleStatusRunning)
		}

		return nil
	}

	run, err := r.ToRun(nil)
	if err != nil {
		return err
	}

	if err := run.Validate(); err != nil {
		return fmt.Errorf("invalid run record: %w", err)
	}

	return nil
}

type ScrubSalt struct {
	SessionName   string
	SaltID        string
	SaltEncrypted []byte
	CreatedAt     time.Time
}

type Baseline struct {
	BaselineID  string
	SessionName string
	SourceRunID string
	GitSHA      string
	CreatedAt   time.Time
}
