package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestManagerLifecycleCreateSnapshotRestoreDestroy(t *testing.T) {
	t.Parallel()

	sqliteStore := openSessionTestStore(t)
	defer sqliteStore.Close()
	manager := newTestManager(t, sqliteStore)

	ctx := context.Background()
	created, err := manager.Create(ctx, "onboarding")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.SessionName != "onboarding" {
		t.Fatalf("Create().SessionName = %q, want onboarding", created.SessionName)
	}

	first, err := manager.Snapshot(ctx, created.SessionName, map[string]any{
		"stripe": map[string]any{
			"customers": []any{"cus_001"},
		},
	})
	if err != nil {
		t.Fatalf("Snapshot(first) error = %v", err)
	}

	second, err := manager.Snapshot(ctx, created.SessionName, map[string]any{
		"stripe": map[string]any{
			"customers": []any{"cus_001", "cus_002"},
		},
	})
	if err != nil {
		t.Fatalf("Snapshot(second) error = %v", err)
	}
	if second.ParentSnapshotID != first.SnapshotID {
		t.Fatalf("second.ParentSnapshotID = %q, want %q", second.ParentSnapshotID, first.SnapshotID)
	}

	restored, err := manager.Restore(ctx, created.SessionName, first.SnapshotID)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if restored.CurrentSnapshotID != first.SnapshotID {
		t.Fatalf("CurrentSnapshotID = %q, want %q", restored.CurrentSnapshotID, first.SnapshotID)
	}

	current, err := manager.CurrentSnapshot(ctx, created.SessionName)
	if err != nil {
		t.Fatalf("CurrentSnapshot() error = %v", err)
	}
	if current.SnapshotID != first.SnapshotID {
		t.Fatalf("CurrentSnapshot().SnapshotID = %q, want %q", current.SnapshotID, first.SnapshotID)
	}

	if err := manager.Destroy(ctx, created.SessionName); err != nil {
		t.Fatalf("Destroy() error = %v", err)
	}
	if _, err := sqliteStore.GetSession(ctx, created.SessionName); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSession(after Destroy) error = %v, want store.ErrNotFound", err)
	}
	if _, err := sqliteStore.GetSessionSnapshot(ctx, first.SnapshotID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSessionSnapshot(after Destroy) error = %v, want store.ErrNotFound", err)
	}
}

func TestManagerForkCopiesCurrentSnapshotAndIsolatesChild(t *testing.T) {
	t.Parallel()

	sqliteStore := openSessionTestStore(t)
	defer sqliteStore.Close()
	manager := newTestManager(t, sqliteStore)

	ctx := context.Background()
	parent, err := manager.Create(ctx, "parent")
	if err != nil {
		t.Fatalf("Create(parent) error = %v", err)
	}
	parentSnapshot, err := manager.Snapshot(ctx, parent.SessionName, map[string]any{
		"service": map[string]any{
			"counter": 1,
		},
	})
	if err != nil {
		t.Fatalf("Snapshot(parent) error = %v", err)
	}

	child, err := manager.Fork(ctx, parent.SessionName, "child")
	if err != nil {
		t.Fatalf("Fork() error = %v", err)
	}
	if child.ParentSessionName != parent.SessionName {
		t.Fatalf("child.ParentSessionName = %q, want %q", child.ParentSessionName, parent.SessionName)
	}
	if child.CurrentSnapshotID == "" || child.CurrentSnapshotID == parentSnapshot.SnapshotID {
		t.Fatalf("child.CurrentSnapshotID = %q, want distinct copied snapshot", child.CurrentSnapshotID)
	}

	childSnapshot, err := manager.CurrentSnapshot(ctx, child.SessionName)
	if err != nil {
		t.Fatalf("CurrentSnapshot(child) error = %v", err)
	}
	if childSnapshot.ParentSnapshotID != parentSnapshot.SnapshotID {
		t.Fatalf("child snapshot parent = %q, want %q", childSnapshot.ParentSnapshotID, parentSnapshot.SnapshotID)
	}

	if _, err := manager.Snapshot(ctx, child.SessionName, map[string]any{
		"service": map[string]any{
			"counter": 2,
		},
	}); err != nil {
		t.Fatalf("Snapshot(child) error = %v", err)
	}

	parentCurrent, err := manager.CurrentSnapshot(ctx, parent.SessionName)
	if err != nil {
		t.Fatalf("CurrentSnapshot(parent) error = %v", err)
	}
	childCurrent, err := manager.CurrentSnapshot(ctx, child.SessionName)
	if err != nil {
		t.Fatalf("CurrentSnapshot(child latest) error = %v", err)
	}

	if parentCurrent.SnapshotID != parentSnapshot.SnapshotID {
		t.Fatalf("parent current snapshot changed to %q, want %q", parentCurrent.SnapshotID, parentSnapshot.SnapshotID)
	}
	if childCurrent.SnapshotID == parentCurrent.SnapshotID {
		t.Fatalf("child and parent share current snapshot %q", childCurrent.SnapshotID)
	}
}

func TestManagerRejectsCrossSessionRestore(t *testing.T) {
	t.Parallel()

	sqliteStore := openSessionTestStore(t)
	defer sqliteStore.Close()
	manager := newTestManager(t, sqliteStore)

	ctx := context.Background()
	first, err := manager.Create(ctx, "first")
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	second, err := manager.Create(ctx, "second")
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	snapshot, err := manager.Snapshot(ctx, first.SessionName, map[string]any{"value": "first"})
	if err != nil {
		t.Fatalf("Snapshot(first) error = %v", err)
	}

	if _, err := manager.Restore(ctx, second.SessionName, snapshot.SnapshotID); err == nil {
		t.Fatal("Restore(cross-session) expected failure")
	}
}

func openSessionTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	return sqliteStore
}

func newTestManager(t *testing.T, sessionStore store.SessionStore) *Manager {
	t.Helper()

	now := time.Date(2026, time.April, 24, 12, 0, 0, 0, time.UTC)
	nextID := 0
	manager, err := NewManager(
		sessionStore,
		WithClock(func() time.Time {
			now = now.Add(time.Second)
			return now
		}),
		WithIDGenerator(func(prefix string) (string, error) {
			nextID++
			return prefix + "_test_" + time.Unix(int64(nextID), 0).UTC().Format("20060102150405"), nil
		}),
	)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager
}
