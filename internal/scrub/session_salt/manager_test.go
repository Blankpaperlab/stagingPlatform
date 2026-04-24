package session_salt

import (
	"bytes"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sqlitestore "stagehand/internal/store/sqlite"
)

func TestNewManagerValidatesMasterKeyLength(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	if _, err := NewManager(sqliteStore, []byte("short")); err == nil {
		t.Fatal("NewManager() expected master key length validation error")
	}
}

func TestManagerGetOrCreateEncryptsSaltAtRestAndReusesSessionSalt(t *testing.T) {
	t.Parallel()

	sqliteStore := openTestStore(t)
	defer sqliteStore.Close()

	manager, err := NewManager(sqliteStore, bytes.Repeat([]byte{0x42}, MasterKeySize))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	manager.random = bytes.NewReader(bytes.Repeat([]byte{0xAB}, SaltSize+8+nonceSize))
	manager.now = func() time.Time {
		return time.Date(2026, time.April, 22, 9, 30, 0, 0, time.UTC)
	}

	first, err := manager.GetOrCreate(context.Background(), "onboarding-flow")
	if err != nil {
		t.Fatalf("GetOrCreate(first) error = %v", err)
	}

	if len(first.Salt) != SaltSize {
		t.Fatalf("len(first.Salt) = %d, want %d", len(first.Salt), SaltSize)
	}

	record, err := sqliteStore.GetScrubSalt(context.Background(), "onboarding-flow")
	if err != nil {
		t.Fatalf("GetScrubSalt() error = %v", err)
	}

	if record.SaltID != first.SaltID {
		t.Fatalf("record.SaltID = %q, want %q", record.SaltID, first.SaltID)
	}

	if bytes.Equal(record.SaltEncrypted, first.Salt) {
		t.Fatal("salt_encrypted should not equal plaintext salt bytes")
	}

	second, err := manager.GetOrCreate(context.Background(), "onboarding-flow")
	if err != nil {
		t.Fatalf("GetOrCreate(second) error = %v", err)
	}

	if second.SaltID != first.SaltID {
		t.Fatalf("second.SaltID = %q, want %q", second.SaltID, first.SaltID)
	}

	if !bytes.Equal(second.Salt, first.Salt) {
		t.Fatal("GetOrCreate() did not reuse the existing session salt")
	}
}

func TestReplacementIsStableWithinSessionAndDiffersAcrossSessions(t *testing.T) {
	t.Parallel()

	sessionA := bytes.Repeat([]byte{0x11}, SaltSize)
	sessionB := bytes.Repeat([]byte{0x22}, SaltSize)

	first, err := Replacement(sessionA, "alice@example.com")
	if err != nil {
		t.Fatalf("Replacement(first) error = %v", err)
	}
	second, err := Replacement(sessionA, "alice@example.com")
	if err != nil {
		t.Fatalf("Replacement(second) error = %v", err)
	}
	third, err := Replacement(sessionB, "alice@example.com")
	if err != nil {
		t.Fatalf("Replacement(third) error = %v", err)
	}

	if first != second {
		t.Fatalf("same-session replacements differ: %q vs %q", first, second)
	}

	if first == third {
		t.Fatalf("cross-session replacements should differ: %q vs %q", first, third)
	}

	if first == "alice@example.com" {
		t.Fatalf("replacement should not keep original value: %q", first)
	}
}

func TestManagerGetOrCreateIsStableAcrossIndependentStores(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	storeA, err := sqlitestore.OpenStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenStore(storeA) error = %v", err)
	}
	defer storeA.Close()

	storeB, err := sqlitestore.OpenStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenStore(storeB) error = %v", err)
	}
	defer storeB.Close()

	managerA, err := NewManager(storeA, bytes.Repeat([]byte{0x24}, MasterKeySize))
	if err != nil {
		t.Fatalf("NewManager(storeA) error = %v", err)
	}
	managerA.random = bytes.NewReader(bytes.Repeat([]byte{0xA1}, SaltSize+8+nonceSize))
	managerA.now = func() time.Time {
		return time.Date(2026, time.April, 23, 10, 0, 0, 0, time.UTC)
	}

	managerB, err := NewManager(storeB, bytes.Repeat([]byte{0x24}, MasterKeySize))
	if err != nil {
		t.Fatalf("NewManager(storeB) error = %v", err)
	}
	managerB.random = bytes.NewReader(bytes.Repeat([]byte{0xB2}, SaltSize+8+nonceSize))
	managerB.now = func() time.Time {
		return time.Date(2026, time.April, 23, 10, 0, 1, 0, time.UTC)
	}

	const sessionName = "cross-process-session"

	type result struct {
		material Material
		err      error
	}

	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, manager := range []*Manager{managerA, managerB} {
		wg.Add(1)
		go func(manager *Manager) {
			defer wg.Done()
			<-start
			material, err := manager.GetOrCreate(context.Background(), sessionName)
			results <- result{material: material, err: err}
		}(manager)
	}

	close(start)

	wg.Wait()
	close(results)

	var materials []Material
	for result := range results {
		if result.err != nil {
			t.Fatalf("GetOrCreate() error = %v", result.err)
		}
		materials = append(materials, result.material)
	}

	if len(materials) != 2 {
		t.Fatalf("len(materials) = %d, want 2", len(materials))
	}

	saltIDs := make(map[string]struct{}, len(materials))
	salts := make(map[string]struct{}, len(materials))
	for _, material := range materials {
		saltIDs[material.SaltID] = struct{}{}
		salts[string(material.Salt)] = struct{}{}
	}

	if len(saltIDs) != 1 {
		t.Fatalf("len(unique salt IDs) = %d, want 1", len(saltIDs))
	}

	if len(salts) != 1 {
		t.Fatalf("len(unique salts) = %d, want 1", len(salts))
	}
}

func openTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "stagehand.db")
	sqliteStore, err := sqlitestore.OpenStore(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	return sqliteStore
}
