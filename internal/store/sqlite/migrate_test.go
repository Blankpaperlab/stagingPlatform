package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
)

func TestOpenAndMigrateCreatesExpectedSchema(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	db, err := OpenAndMigrate(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenAndMigrate() error = %v", err)
	}
	defer db.Close()

	tables := objectNames(t, db, "table")
	indexes := objectNames(t, db, "index")

	for _, want := range []string{
		"assertions",
		"baselines",
		"events",
		"interactions",
		"runs",
		"schema_migrations",
		"scrub_salts",
	} {
		if !slices.Contains(tables, want) {
			t.Fatalf("expected table %q to exist, tables = %v", want, tables)
		}
	}

	for _, want := range []string{
		"idx_assertions_run_id",
		"idx_baselines_session_created_at",
		"idx_events_interaction_id",
		"idx_events_interaction_sequence",
		"idx_interactions_parent_id",
		"idx_interactions_run_id",
		"idx_interactions_run_sequence",
		"idx_runs_session_started_at",
		"idx_runs_status_started_at",
		"idx_scrub_salts_salt_id",
	} {
		if !slices.Contains(indexes, want) {
			t.Fatalf("expected index %q to exist, indexes = %v", want, indexes)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	db, err := OpenAndMigrate(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenAndMigrate() error = %v", err)
	}
	defer db.Close()

	if err := Migrate(context.Background(), db); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("QueryRow() error = %v", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}

	if count != len(migrations) {
		t.Fatalf("schema_migrations row count = %d, want %d", count, len(migrations))
	}
}

func TestOpenRejectsEmptyPath(t *testing.T) {
	t.Parallel()

	if _, err := Open(""); err == nil {
		t.Fatal("Open() expected empty path failure")
	}
}

func objectNames(t *testing.T, db *sql.DB, kind string) []string {
	t.Helper()

	rows, err := db.Query(
		`SELECT name FROM sqlite_master WHERE type = ? AND name NOT LIKE 'sqlite_%' ORDER BY name`,
		kind,
	)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("rows.Scan() error = %v", err)
		}
		names = append(names, name)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() error = %v", err)
	}

	return names
}
