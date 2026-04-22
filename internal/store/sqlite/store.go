package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"stagehand/internal/recorder"
	"stagehand/internal/store"
)

const sqliteTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

var _ store.ArtifactStore = (*Store)(nil)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlite database handle is required")
	}

	return &Store{db: db}, nil
}

func OpenStore(ctx context.Context, path string) (*Store, error) {
	db, err := OpenAndMigrate(ctx, path)
	if err != nil {
		return nil, err
	}

	return NewStore(db)
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) CreateRun(ctx context.Context, run store.RunRecord) error {
	if err := run.Validate(); err != nil {
		return fmt.Errorf("validate run record %q: %w", run.RunID, err)
	}

	integrityJSON, err := marshalJSON(run.IntegrityIssues)
	if err != nil {
		return fmt.Errorf("marshal run integrity issues for %q: %w", run.RunID, err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO runs (
			run_id, session_name, mode, status, schema_version, sdk_version, runtime_version,
			scrub_policy_version, base_snapshot_id, agent_version, git_sha, started_at, ended_at, integrity_issues_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID,
		run.SessionName,
		string(run.Mode),
		string(run.Status),
		run.SchemaVersion,
		run.SDKVersion,
		run.RuntimeVersion,
		run.ScrubPolicyVersion,
		nullIfEmpty(run.BaseSnapshotID),
		nullIfEmpty(run.AgentVersion),
		nullIfEmpty(run.GitSHA),
		formatTime(run.StartedAt),
		formatOptionalTime(run.EndedAt),
		string(integrityJSON),
	)
	if err != nil {
		return fmt.Errorf("insert run %q: %w", run.RunID, err)
	}

	return nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (recorder.Run, error) {
	runRecord, err := s.getRunRecord(ctx, runID)
	if err != nil {
		return recorder.Run{}, err
	}

	interactions, err := s.ListInteractions(ctx, runID)
	if err != nil {
		return recorder.Run{}, err
	}

	run, err := runRecord.ToRun(interactions)
	if err != nil {
		return recorder.Run{}, fmt.Errorf("hydrate run %q: %w", runID, err)
	}

	if err := run.Validate(); err != nil {
		return recorder.Run{}, fmt.Errorf("hydrate run %q: %w", runID, err)
	}

	return run, nil
}

func (s *Store) GetRunRecord(ctx context.Context, runID string) (store.RunRecord, error) {
	return s.getRunRecord(ctx, runID)
}

func (s *Store) UpdateRun(ctx context.Context, run store.RunRecord) error {
	if err := run.Validate(); err != nil {
		return fmt.Errorf("validate run record %q: %w", run.RunID, err)
	}

	integrityJSON, err := marshalJSON(run.IntegrityIssues)
	if err != nil {
		return fmt.Errorf("marshal run integrity issues for %q: %w", run.RunID, err)
	}

	result, err := s.db.ExecContext(
		ctx,
		`UPDATE runs SET
			session_name = ?,
			mode = ?,
			status = ?,
			schema_version = ?,
			sdk_version = ?,
			runtime_version = ?,
			scrub_policy_version = ?,
			base_snapshot_id = ?,
			agent_version = ?,
			git_sha = ?,
			started_at = ?,
			ended_at = ?,
			integrity_issues_json = ?
		WHERE run_id = ?`,
		run.SessionName,
		string(run.Mode),
		string(run.Status),
		run.SchemaVersion,
		run.SDKVersion,
		run.RuntimeVersion,
		run.ScrubPolicyVersion,
		nullIfEmpty(run.BaseSnapshotID),
		nullIfEmpty(run.AgentVersion),
		nullIfEmpty(run.GitSHA),
		formatTime(run.StartedAt),
		formatOptionalTime(run.EndedAt),
		string(integrityJSON),
		run.RunID,
	)
	if err != nil {
		return fmt.Errorf("update run %q: %w", run.RunID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return fmt.Errorf("update run %q: %w", run.RunID, store.ErrNotFound)
	}

	return nil
}

func (s *Store) DeleteRun(ctx context.Context, runID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM runs WHERE run_id = ?`, runID)
	if err != nil {
		return fmt.Errorf("delete run %q: %w", runID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		return fmt.Errorf("delete run %q: %w", runID, store.ErrNotFound)
	}

	return nil
}

func (s *Store) DeleteSession(ctx context.Context, sessionName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete session %q: %w", sessionName, err)
	}

	var (
		runCount  int
		saltCount int
	)

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE session_name = ?`, sessionName).Scan(&runCount); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("count runs for session %q: %w", sessionName, err)
	}

	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scrub_salts WHERE session_name = ?`, sessionName).Scan(&saltCount); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("count scrub salts for session %q: %w", sessionName, err)
	}

	if runCount == 0 && saltCount == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("delete session %q: %w", sessionName, store.ErrNotFound)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM baselines WHERE session_name = ?`, sessionName); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete baselines for session %q: %w", sessionName, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM runs WHERE session_name = ?`, sessionName); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete runs for session %q: %w", sessionName, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM scrub_salts WHERE session_name = ?`, sessionName); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete scrub salts for session %q: %w", sessionName, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete session %q: %w", sessionName, err)
	}

	return nil
}

func (s *Store) WriteInteraction(ctx context.Context, interaction recorder.Interaction) error {
	if strings.TrimSpace(interaction.RunID) == "" {
		return fmt.Errorf("interaction run_id is required")
	}

	runRecord, err := s.getRunRecord(ctx, interaction.RunID)
	if err != nil {
		return err
	}

	if err := interaction.Validate(runRecord.RunID, runRecord.ScrubPolicyVersion); err != nil {
		return fmt.Errorf("validate interaction %q: %w", interaction.InteractionID, err)
	}

	requestJSON, err := marshalJSON(interaction.Request)
	if err != nil {
		return fmt.Errorf("marshal request for interaction %q: %w", interaction.InteractionID, err)
	}

	scrubReportJSON, err := marshalJSON(interaction.ScrubReport)
	if err != nil {
		return fmt.Errorf("marshal scrub report for interaction %q: %w", interaction.InteractionID, err)
	}

	extractedEntitiesJSON, err := marshalJSON(interaction.ExtractedEntities)
	if err != nil {
		return fmt.Errorf("marshal extracted entities for interaction %q: %w", interaction.InteractionID, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin interaction write %q: %w", interaction.InteractionID, err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO interactions (
			interaction_id, run_id, parent_interaction_id, sequence, service, operation, protocol,
			streaming, fallback_tier, request_json, scrub_report_json, extracted_entities_json, latency_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(interaction_id) DO UPDATE SET
			run_id = excluded.run_id,
			parent_interaction_id = excluded.parent_interaction_id,
			sequence = excluded.sequence,
			service = excluded.service,
			operation = excluded.operation,
			protocol = excluded.protocol,
			streaming = excluded.streaming,
			fallback_tier = excluded.fallback_tier,
			request_json = excluded.request_json,
			scrub_report_json = excluded.scrub_report_json,
			extracted_entities_json = excluded.extracted_entities_json,
			latency_ms = excluded.latency_ms`,
		interaction.InteractionID,
		interaction.RunID,
		nullIfEmpty(interaction.ParentInteractionID),
		interaction.Sequence,
		interaction.Service,
		interaction.Operation,
		string(interaction.Protocol),
		boolToInt(interaction.Streaming),
		nullIfEmpty(string(interaction.FallbackTier)),
		string(requestJSON),
		string(scrubReportJSON),
		string(extractedEntitiesJSON),
		nullableInt64(interaction.LatencyMS),
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("upsert interaction %q: %w", interaction.InteractionID, err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE interaction_id = ?`, interaction.InteractionID); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete existing events for interaction %q: %w", interaction.InteractionID, err)
	}

	for _, event := range interaction.Events {
		eventJSON, err := marshalJSON(event.Data)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("marshal event data for interaction %q sequence %d: %w", interaction.InteractionID, event.Sequence, err)
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO events (
				event_id, interaction_id, sequence, t_ms, sim_t_ms, type, data_json, nested_interaction_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			eventID(interaction.InteractionID, event.Sequence),
			interaction.InteractionID,
			event.Sequence,
			event.TMS,
			event.SimTMS,
			string(event.Type),
			nullableJSONString(eventJSON),
			nullIfEmpty(event.NestedInteractionID),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert event for interaction %q sequence %d: %w", interaction.InteractionID, event.Sequence, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit interaction write %q: %w", interaction.InteractionID, err)
	}

	return nil
}

func (s *Store) ListInteractions(ctx context.Context, runID string) ([]recorder.Interaction, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			interaction_id, run_id, parent_interaction_id, sequence, service, operation, protocol,
			streaming, fallback_tier, request_json, scrub_report_json, extracted_entities_json, latency_ms
		FROM interactions
		WHERE run_id = ?
		ORDER BY sequence ASC`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("query interactions for run %q: %w", runID, err)
	}

	type interactionRow struct {
		interactionID       string
		recordRunID         string
		parentInteractionID sql.NullString
		sequence            int
		service             string
		operation           string
		protocol            string
		streaming           int
		fallbackTier        sql.NullString
		requestJSON         string
		scrubReportJSON     string
		extractedEntities   sql.NullString
		latencyMS           sql.NullInt64
	}

	var scannedRows []interactionRow
	for rows.Next() {
		var scanned interactionRow
		if err := rows.Scan(
			&scanned.interactionID,
			&scanned.recordRunID,
			&scanned.parentInteractionID,
			&scanned.sequence,
			&scanned.service,
			&scanned.operation,
			&scanned.protocol,
			&scanned.streaming,
			&scanned.fallbackTier,
			&scanned.requestJSON,
			&scanned.scrubReportJSON,
			&scanned.extractedEntities,
			&scanned.latencyMS,
		); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan interaction for run %q: %w", runID, err)
		}

		scannedRows = append(scannedRows, scanned)
	}

	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("iterate interactions for run %q: %w", runID, err)
	}

	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close interaction rows for run %q: %w", runID, err)
	}

	interactions := make([]recorder.Interaction, 0, len(scannedRows))
	for _, scanned := range scannedRows {
		var request recorder.Request
		if err := unmarshalJSONString(scanned.requestJSON, &request); err != nil {
			return nil, fmt.Errorf("decode request for interaction %q: %w", scanned.interactionID, err)
		}

		var scrubReport recorder.ScrubReport
		if err := unmarshalJSONString(scanned.scrubReportJSON, &scrubReport); err != nil {
			return nil, fmt.Errorf("decode scrub report for interaction %q: %w", scanned.interactionID, err)
		}

		var extracted []recorder.ExtractedEntity
		if scanned.extractedEntities.Valid && strings.TrimSpace(scanned.extractedEntities.String) != "" {
			if err := unmarshalJSONString(scanned.extractedEntities.String, &extracted); err != nil {
				return nil, fmt.Errorf("decode extracted entities for interaction %q: %w", scanned.interactionID, err)
			}
		}

		events, err := s.listEventsForInteraction(ctx, scanned.interactionID)
		if err != nil {
			return nil, err
		}

		interaction := recorder.Interaction{
			RunID:               scanned.recordRunID,
			InteractionID:       scanned.interactionID,
			ParentInteractionID: scanned.parentInteractionID.String,
			Sequence:            scanned.sequence,
			Service:             scanned.service,
			Operation:           scanned.operation,
			Protocol:            recorder.Protocol(scanned.protocol),
			Streaming:           scanned.streaming == 1,
			FallbackTier:        recorder.FallbackTier(scanned.fallbackTier.String),
			Request:             request,
			Events:              events,
			ExtractedEntities:   extracted,
			ScrubReport:         scrubReport,
		}
		if scanned.latencyMS.Valid {
			interaction.LatencyMS = scanned.latencyMS.Int64
		}

		interactions = append(interactions, interaction)
	}

	return interactions, nil
}

func (s *Store) PutScrubSalt(ctx context.Context, salt store.ScrubSalt) error {
	if strings.TrimSpace(salt.SessionName) == "" {
		return fmt.Errorf("scrub salt session_name is required")
	}

	if strings.TrimSpace(salt.SaltID) == "" {
		return fmt.Errorf("scrub salt salt_id is required")
	}

	if salt.CreatedAt.IsZero() {
		return fmt.Errorf("scrub salt created_at is required")
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO scrub_salts(session_name, salt_id, salt_encrypted, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(session_name) DO UPDATE SET
			salt_id = excluded.salt_id,
			salt_encrypted = excluded.salt_encrypted,
			created_at = excluded.created_at`,
		salt.SessionName,
		salt.SaltID,
		salt.SaltEncrypted,
		formatTime(salt.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert scrub salt for session %q: %w", salt.SessionName, err)
	}

	return nil
}

func (s *Store) GetScrubSalt(ctx context.Context, sessionName string) (store.ScrubSalt, error) {
	var (
		saltID    string
		saltBytes []byte
		createdAt string
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT salt_id, salt_encrypted, created_at FROM scrub_salts WHERE session_name = ?`,
		sessionName,
	).Scan(&saltID, &saltBytes, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ScrubSalt{}, fmt.Errorf("get scrub salt for session %q: %w", sessionName, store.ErrNotFound)
	}
	if err != nil {
		return store.ScrubSalt{}, fmt.Errorf("get scrub salt for session %q: %w", sessionName, err)
	}

	created, err := parseStoredTime(createdAt)
	if err != nil {
		return store.ScrubSalt{}, fmt.Errorf("parse scrub salt created_at for session %q: %w", sessionName, err)
	}

	return store.ScrubSalt{
		SessionName:   sessionName,
		SaltID:        saltID,
		SaltEncrypted: saltBytes,
		CreatedAt:     created,
	}, nil
}

func (s *Store) PutBaseline(ctx context.Context, baseline store.Baseline) error {
	if strings.TrimSpace(baseline.BaselineID) == "" {
		return fmt.Errorf("baseline_id is required")
	}

	if strings.TrimSpace(baseline.SessionName) == "" {
		return fmt.Errorf("baseline session_name is required")
	}

	if strings.TrimSpace(baseline.SourceRunID) == "" {
		return fmt.Errorf("baseline source_run_id is required")
	}

	if strings.TrimSpace(baseline.GitSHA) == "" {
		return fmt.Errorf("baseline git_sha is required")
	}

	if baseline.CreatedAt.IsZero() {
		return fmt.Errorf("baseline created_at is required")
	}

	runRecord, err := s.getRunRecord(ctx, baseline.SourceRunID)
	if err != nil {
		return err
	}

	if runRecord.Status != store.RunLifecycleStatusComplete {
		return fmt.Errorf("baseline source run %q must be %q, got %q", baseline.SourceRunID, store.RunLifecycleStatusComplete, runRecord.Status)
	}

	if baseline.SessionName != runRecord.SessionName {
		return fmt.Errorf("baseline session %q must match source run session %q", baseline.SessionName, runRecord.SessionName)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO baselines(baseline_id, session_name, source_run_id, git_sha, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(baseline_id) DO UPDATE SET
			session_name = excluded.session_name,
			source_run_id = excluded.source_run_id,
			git_sha = excluded.git_sha,
			created_at = excluded.created_at`,
		baseline.BaselineID,
		baseline.SessionName,
		baseline.SourceRunID,
		baseline.GitSHA,
		formatTime(baseline.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert baseline %q: %w", baseline.BaselineID, err)
	}

	return nil
}

func (s *Store) GetBaseline(ctx context.Context, baselineID string) (store.Baseline, error) {
	return s.getBaselineByQuery(
		ctx,
		`SELECT baseline_id, session_name, source_run_id, git_sha, created_at FROM baselines WHERE baseline_id = ?`,
		baselineID,
	)
}

func (s *Store) GetLatestBaseline(ctx context.Context, sessionName string) (store.Baseline, error) {
	return s.getBaselineByQuery(
		ctx,
		`SELECT baseline_id, session_name, source_run_id, git_sha, created_at
		FROM baselines
		WHERE session_name = ?
		ORDER BY created_at DESC, baseline_id DESC
		LIMIT 1`,
		sessionName,
	)
}

func (s *Store) getRunRecord(ctx context.Context, runID string) (store.RunRecord, error) {
	var (
		record         store.RunRecord
		baseSnapshotID sql.NullString
		agentVersion   sql.NullString
		gitSHA         sql.NullString
		endedAt        sql.NullString
		integrityJSON  string
		startedAt      string
	)

	err := s.db.QueryRowContext(
		ctx,
		`SELECT
			schema_version, sdk_version, runtime_version, scrub_policy_version,
			run_id, session_name, mode, status, base_snapshot_id, agent_version, git_sha,
			started_at, ended_at, integrity_issues_json
		FROM runs
		WHERE run_id = ?`,
		runID,
	).Scan(
		&record.SchemaVersion,
		&record.SDKVersion,
		&record.RuntimeVersion,
		&record.ScrubPolicyVersion,
		&record.RunID,
		&record.SessionName,
		&record.Mode,
		&record.Status,
		&baseSnapshotID,
		&agentVersion,
		&gitSHA,
		&startedAt,
		&endedAt,
		&integrityJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.RunRecord{}, fmt.Errorf("get run %q: %w", runID, store.ErrNotFound)
	}
	if err != nil {
		return store.RunRecord{}, fmt.Errorf("get run %q: %w", runID, err)
	}

	record.BaseSnapshotID = baseSnapshotID.String
	record.AgentVersion = agentVersion.String
	record.GitSHA = gitSHA.String

	parsedStartedAt, err := parseStoredTime(startedAt)
	if err != nil {
		return store.RunRecord{}, fmt.Errorf("parse started_at for run %q: %w", runID, err)
	}
	record.StartedAt = parsedStartedAt

	if endedAt.Valid && strings.TrimSpace(endedAt.String) != "" {
		parsedEndedAt, err := parseStoredTime(endedAt.String)
		if err != nil {
			return store.RunRecord{}, fmt.Errorf("parse ended_at for run %q: %w", runID, err)
		}
		record.EndedAt = &parsedEndedAt
	}

	if err := unmarshalJSONString(integrityJSON, &record.IntegrityIssues); err != nil {
		return store.RunRecord{}, fmt.Errorf("decode integrity issues for run %q: %w", runID, err)
	}

	return record, nil
}

func (s *Store) listEventsForInteraction(ctx context.Context, interactionID string) ([]recorder.Event, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT sequence, t_ms, sim_t_ms, type, data_json, nested_interaction_id
		FROM events
		WHERE interaction_id = ?
		ORDER BY sequence ASC`,
		interactionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query events for interaction %q: %w", interactionID, err)
	}
	defer rows.Close()

	var events []recorder.Event
	for rows.Next() {
		var (
			event               recorder.Event
			eventType           string
			dataJSON            sql.NullString
			nestedInteractionID sql.NullString
		)

		if err := rows.Scan(&event.Sequence, &event.TMS, &event.SimTMS, &eventType, &dataJSON, &nestedInteractionID); err != nil {
			return nil, fmt.Errorf("scan events for interaction %q: %w", interactionID, err)
		}

		event.Type = recorder.EventType(eventType)
		event.NestedInteractionID = nestedInteractionID.String

		if dataJSON.Valid && strings.TrimSpace(dataJSON.String) != "" {
			if err := unmarshalJSONString(dataJSON.String, &event.Data); err != nil {
				return nil, fmt.Errorf("decode event data for interaction %q sequence %d: %w", interactionID, event.Sequence, err)
			}
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events for interaction %q: %w", interactionID, err)
	}

	return events, nil
}

func (s *Store) getBaselineByQuery(ctx context.Context, query, value string) (store.Baseline, error) {
	var (
		baseline  store.Baseline
		createdAt string
	)

	err := s.db.QueryRowContext(ctx, query, value).Scan(
		&baseline.BaselineID,
		&baseline.SessionName,
		&baseline.SourceRunID,
		&baseline.GitSHA,
		&createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Baseline{}, fmt.Errorf("baseline lookup %q: %w", value, store.ErrNotFound)
	}
	if err != nil {
		return store.Baseline{}, fmt.Errorf("baseline lookup %q: %w", value, err)
	}

	parsedCreatedAt, err := parseStoredTime(createdAt)
	if err != nil {
		return store.Baseline{}, fmt.Errorf("parse baseline created_at for %q: %w", value, err)
	}
	baseline.CreatedAt = parsedCreatedAt

	return baseline, nil
}

func (s *Store) ensureRunExists(ctx context.Context, runID string) error {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE run_id = ?`, runID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("run %q: %w", runID, store.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("check run %q existence: %w", runID, err)
	}

	return nil
}

func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func unmarshalJSONString(raw string, target any) error {
	return json.Unmarshal([]byte(raw), target)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(sqliteTimeFormat)
}

func formatOptionalTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}

	return formatTime(*t)
}

func parseStoredTime(value string) (time.Time, error) {
	return time.Parse(sqliteTimeFormat, value)
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}

	return value
}

func nullableJSONString(value []byte) any {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}

	return string(value)
}

func eventID(interactionID string, sequence int) string {
	return fmt.Sprintf("%s:%08d", interactionID, sequence)
}
