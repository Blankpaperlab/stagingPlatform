package recording_test

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/recording"
	"stagehand/internal/scrub/session_salt"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"

	_ "modernc.org/sqlite"
)

func TestNewWriterRejectsDisabledScrub(t *testing.T) {
	t.Parallel()

	sqliteStore := openStore(t)
	defer sqliteStore.Close()

	saltManager := newSaltManager(t, sqliteStore)
	cfg := config.DefaultConfig()
	cfg.Scrub.Enabled = false

	_, err := recording.NewWriter(recording.WriterOptions{
		Store:       sqliteStore,
		SaltManager: saltManager,
		ScrubConfig: cfg.Scrub,
	})
	if err == nil || !strings.Contains(err.Error(), "scrub.enabled must be true") {
		t.Fatalf("NewWriter() error = %v, want scrub.enabled validation failure", err)
	}
}

func TestWriterPersistsScrubbedInteractionWithoutPlaintextSecrets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer sqliteStore.Close()

	run := validRunningRunRecord()
	if err := sqliteStore.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	cfg := config.DefaultConfig()
	saltManager := newSaltManager(t, sqliteStore)
	writer, err := recording.NewWriter(recording.WriterOptions{
		Store:       sqliteStore,
		SaltManager: saltManager,
		ScrubConfig: cfg.Scrub,
	})
	if err != nil {
		t.Fatalf("NewWriter() error = %v", err)
	}

	raw := sensitiveInteraction(run.RunID)
	scrubbed, err := writer.PersistInteraction(ctx, raw)
	if err != nil {
		t.Fatalf("PersistInteraction() error = %v", err)
	}

	hashedEmail := scrubbed.Request.Headers["x-customer-email"][0]
	if hashedEmail == "alice@example.com" {
		t.Fatal("PersistInteraction() preserved plaintext email in scrubbed interaction")
	}

	if _, ok := scrubbed.Request.Headers["authorization"]; ok {
		t.Fatal("authorization header was persisted in scrubbed interaction")
	}
	if _, ok := scrubbed.Request.Headers["cookie"]; ok {
		t.Fatal("cookie header was persisted in scrubbed interaction")
	}

	scrubbedNotes := scrubbed.Request.Body.(map[string]any)["notes"].(string)
	if !strings.Contains(scrubbedNotes, hashedEmail) {
		t.Fatalf("scrubbed notes = %q, want hashed email %q reused", scrubbedNotes, hashedEmail)
	}

	eventMessage := scrubbed.Events[1].Data["message"].(string)
	if !strings.Contains(eventMessage, hashedEmail) {
		t.Fatalf("scrubbed event message = %q, want hashed email %q reused", eventMessage, hashedEmail)
	}

	if !strings.Contains(scrubbed.Request.URL, url.QueryEscape(hashedEmail)) {
		t.Fatalf("scrubbed URL = %q, want hashed email %q in query string", scrubbed.Request.URL, hashedEmail)
	}

	requestJSON, eventJSON := rawStoredJSON(t, dbPath, raw.InteractionID)
	persisted := requestJSON + "\n" + eventJSON

	for _, leak := range []string{
		"alice@example.com",
		"Bearer " + fakeStripeKey(),
		"session=secret-cookie",
		"+1 (415) 555-2671",
		"123-45-6789",
		"4242 4242 4242 4242",
		fakeStripeKey(),
		fakeOpenAIProjectKey(),
		fakeJWT(),
	} {
		if strings.Contains(persisted, leak) {
			t.Fatalf("persisted SQLite JSON still contains plaintext secret %q:\n%s", leak, persisted)
		}
	}

	for _, unexpected := range []string{`"authorization"`, `"cookie"`} {
		if strings.Contains(requestJSON, unexpected) {
			t.Fatalf("request_json still contains dropped header key %q:\n%s", unexpected, requestJSON)
		}
	}

	if !strings.Contains(persisted, hashedEmail) {
		t.Fatalf("persisted SQLite JSON = %q, want hashed email %q present", persisted, hashedEmail)
	}

	wantPaths := []string{
		"request.headers.authorization",
		"request.headers.cookie",
		"request.headers.x-customer-email",
		"request.query.email",
		"request.query.token",
		"request.body.notes",
		"events[1].data.message",
	}
	if got := scrubbed.ScrubReport.RedactedPaths; strings.Join(got, "|") != strings.Join(wantPaths, "|") {
		t.Fatalf("ScrubReport.RedactedPaths = %#v, want %#v", got, wantPaths)
	}
}

func openStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	return sqliteStore
}

func newSaltManager(t *testing.T, artifactStore store.ArtifactStore) *session_salt.Manager {
	t.Helper()

	manager, err := session_salt.NewManager(artifactStore, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager
}

func validRunningRunRecord() store.RunRecord {
	startedAt := time.Date(2026, time.April, 22, 10, 0, 0, 0, time.UTC)

	return store.RunRecord{
		SchemaVersion:      recorder.ArtifactSchemaVersion,
		SDKVersion:         "0.1.0-alpha.0",
		RuntimeVersion:     "0.1.0-alpha.0",
		ScrubPolicyVersion: "v1",
		RunID:              "run_recording_safe_001",
		SessionName:        "onboarding-flow",
		Mode:               recorder.RunModeRecord,
		Status:             store.RunLifecycleStatusRunning,
		AgentVersion:       "agent-v1",
		GitSHA:             "abc123",
		StartedAt:          startedAt,
	}
}

func sensitiveInteraction(runID string) recorder.Interaction {
	return recorder.Interaction{
		RunID:         runID,
		InteractionID: "int_recording_safe_001",
		Sequence:      1,
		Service:       "openai",
		Operation:     "chat.completions.create",
		Protocol:      recorder.ProtocolHTTPS,
		Request: recorder.Request{
			URL:    "https://api.openai.com/v1/chat/completions?email=alice@example.com&token=" + url.QueryEscape(fakeOpenAIProjectKey()),
			Method: "POST",
			Headers: map[string][]string{
				"authorization":    {"Bearer " + fakeStripeKey()},
				"cookie":           {"session=secret-cookie"},
				"x-customer-email": {"alice@example.com"},
				"content-type":     {"application/json"},
			},
			Body: map[string]any{
				"notes": "Email alice@example.com, call +1 (415) 555-2671, SSN 123-45-6789, card 4242 4242 4242 4242, key " + fakeStripeKey() + ".",
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
				TMS:      250,
				SimTMS:   250,
				Type:     recorder.EventTypeResponseReceived,
				Data: map[string]any{
					"message": "JWT " + fakeJWT() + " returned for alice@example.com",
				},
			},
		},
		ScrubReport: recorder.ScrubReport{
			ScrubPolicyVersion: "placeholder",
			SessionSaltID:      "placeholder",
		},
		LatencyMS: 250,
	}
}

func rawStoredJSON(t *testing.T, path, interactionID string) (string, string) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	var requestJSON string
	if err := db.QueryRow(`SELECT request_json FROM interactions WHERE interaction_id = ?`, interactionID).Scan(&requestJSON); err != nil {
		t.Fatalf("query request_json error = %v", err)
	}

	rows, err := db.Query(`SELECT data_json FROM events WHERE interaction_id = ? ORDER BY sequence ASC`, interactionID)
	if err != nil {
		t.Fatalf("query event data error = %v", err)
	}
	defer rows.Close()

	var chunks []string
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan event data error = %v", err)
		}
		if raw.Valid {
			chunks = append(chunks, raw.String)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate event data error = %v", err)
	}

	return requestJSON, strings.Join(chunks, "\n")
}

func fakeStripeKey() string {
	return "sk" + "_live_" + "1234567890abcdef1234567890abcdef"
}

func fakeOpenAIProjectKey() string {
	return "sk" + "-proj-" + "abcdefghijklmnopQRST_uvwx"
}

func fakeJWT() string {
	return "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9" + ".eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkFsaWNlIn0.c2lnbmF0dXJl"
}
