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
	CreateScrubSaltIfAbsent(ctx context.Context, salt ScrubSalt) (ScrubSalt, bool, error)
	GetScrubSalt(ctx context.Context, sessionName string) (ScrubSalt, error)
	PutBaseline(ctx context.Context, baseline Baseline) error
	GetBaseline(ctx context.Context, baselineID string) (Baseline, error)
	GetLatestBaseline(ctx context.Context, sessionName string) (Baseline, error)
	Close() error
}

type SessionStore interface {
	CreateSession(ctx context.Context, session SessionRecord) error
	GetSession(ctx context.Context, sessionName string) (SessionRecord, error)
	UpdateSession(ctx context.Context, session SessionRecord) error
	PutSessionSnapshot(ctx context.Context, snapshot SessionSnapshot) error
	GetSessionSnapshot(ctx context.Context, snapshotID string) (SessionSnapshot, error)
	GetLatestSessionSnapshot(ctx context.Context, sessionName string) (SessionSnapshot, error)
	DeleteSession(ctx context.Context, sessionName string) error
	Close() error
}

type EventQueueStore interface {
	PutSessionClock(ctx context.Context, clock SessionClock) error
	GetSessionClock(ctx context.Context, sessionName string) (SessionClock, error)
	PutScheduledEvent(ctx context.Context, event ScheduledEvent) error
	GetScheduledEvent(ctx context.Context, eventID string) (ScheduledEvent, error)
	ListDueScheduledEvents(
		ctx context.Context,
		sessionName string,
		deliveryMode ScheduledEventDeliveryMode,
		dueAt time.Time,
	) ([]ScheduledEvent, error)
	MarkScheduledEventsDelivered(ctx context.Context, sessionName string, eventIDs []string, deliveredAt time.Time) error
	DeleteSession(ctx context.Context, sessionName string) error
	Close() error
}

type SessionStatus string

const (
	SessionStatusActive SessionStatus = "active"
)

var validSessionStatuses = []SessionStatus{
	SessionStatusActive,
}

type SessionRecord struct {
	SessionName       string
	ParentSessionName string
	CurrentSnapshotID string
	Status            SessionStatus
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (s SessionRecord) Validate() error {
	var problems []string

	if strings.TrimSpace(s.SessionName) == "" {
		problems = append(problems, "session_name is required")
	}

	if !slices.Contains(validSessionStatuses, s.Status) {
		problems = append(problems, fmt.Sprintf("status must be %q", SessionStatusActive))
	}

	if s.CreatedAt.IsZero() {
		problems = append(problems, "created_at is required")
	}

	if s.UpdatedAt.IsZero() {
		problems = append(problems, "updated_at is required")
	}

	if !s.CreatedAt.IsZero() && !s.UpdatedAt.IsZero() && s.UpdatedAt.Before(s.CreatedAt) {
		problems = append(problems, "updated_at cannot be before created_at")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid session record: %s", strings.Join(problems, "; "))
	}

	return nil
}

type SessionSnapshot struct {
	SnapshotID       string
	SessionName      string
	ParentSnapshotID string
	SourceRunID      string
	State            map[string]any
	CreatedAt        time.Time
}

func (s SessionSnapshot) Validate() error {
	var problems []string

	if strings.TrimSpace(s.SnapshotID) == "" {
		problems = append(problems, "snapshot_id is required")
	}

	if strings.TrimSpace(s.SessionName) == "" {
		problems = append(problems, "session_name is required")
	}

	if s.State == nil {
		problems = append(problems, "state is required")
	}

	if s.CreatedAt.IsZero() {
		problems = append(problems, "created_at is required")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid session snapshot: %s", strings.Join(problems, "; "))
	}

	return nil
}

type SessionClock struct {
	SessionName string
	CurrentTime time.Time
	UpdatedAt   time.Time
}

func (c SessionClock) Validate() error {
	var problems []string

	if strings.TrimSpace(c.SessionName) == "" {
		problems = append(problems, "session_name is required")
	}

	if c.CurrentTime.IsZero() {
		problems = append(problems, "current_time is required")
	}

	if c.UpdatedAt.IsZero() {
		problems = append(problems, "updated_at is required")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid session clock: %s", strings.Join(problems, "; "))
	}

	return nil
}

type ScheduledEventDeliveryMode string

const (
	ScheduledEventDeliveryModePush ScheduledEventDeliveryMode = "push"
	ScheduledEventDeliveryModePull ScheduledEventDeliveryMode = "pull"
)

var validScheduledEventDeliveryModes = []ScheduledEventDeliveryMode{
	ScheduledEventDeliveryModePush,
	ScheduledEventDeliveryModePull,
}

type ScheduledEventStatus string

const (
	ScheduledEventStatusPending   ScheduledEventStatus = "pending"
	ScheduledEventStatusDelivered ScheduledEventStatus = "delivered"
)

var validScheduledEventStatuses = []ScheduledEventStatus{
	ScheduledEventStatusPending,
	ScheduledEventStatusDelivered,
}

type ScheduledEvent struct {
	EventID      string
	SessionName  string
	Service      string
	Topic        string
	DeliveryMode ScheduledEventDeliveryMode
	DueAt        time.Time
	Payload      map[string]any
	Status       ScheduledEventStatus
	CreatedAt    time.Time
	DeliveredAt  *time.Time
}

func (e ScheduledEvent) Validate() error {
	var problems []string

	if strings.TrimSpace(e.EventID) == "" {
		problems = append(problems, "event_id is required")
	}

	if strings.TrimSpace(e.SessionName) == "" {
		problems = append(problems, "session_name is required")
	}

	if strings.TrimSpace(e.Service) == "" {
		problems = append(problems, "service is required")
	}

	if strings.TrimSpace(e.Topic) == "" {
		problems = append(problems, "topic is required")
	}

	if !slices.Contains(validScheduledEventDeliveryModes, e.DeliveryMode) {
		problems = append(problems, fmt.Sprintf("delivery_mode must be one of %q, %q", ScheduledEventDeliveryModePush, ScheduledEventDeliveryModePull))
	}

	if e.DueAt.IsZero() {
		problems = append(problems, "due_at is required")
	}

	if e.Payload == nil {
		problems = append(problems, "payload is required")
	}

	if !slices.Contains(validScheduledEventStatuses, e.Status) {
		problems = append(problems, fmt.Sprintf("status must be one of %q, %q", ScheduledEventStatusPending, ScheduledEventStatusDelivered))
	}

	if e.CreatedAt.IsZero() {
		problems = append(problems, "created_at is required")
	}

	if e.Status == ScheduledEventStatusPending && e.DeliveredAt != nil {
		problems = append(problems, "delivered_at must be empty when status is pending")
	}

	if e.Status == ScheduledEventStatusDelivered && (e.DeliveredAt == nil || e.DeliveredAt.IsZero()) {
		problems = append(problems, "delivered_at is required when status is delivered")
	}

	if len(problems) > 0 {
		return fmt.Errorf("invalid scheduled event: %s", strings.Join(problems, "; "))
	}

	return nil
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
