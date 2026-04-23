package session_salt

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"stagehand/internal/recorder"
	"stagehand/internal/store"
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

func TestManagerGetOrCreateSerializesConcurrentCreationPerSession(t *testing.T) {
	store := newBlockingScrubSaltStore(1)
	manager, err := NewManager(store, bytes.Repeat([]byte{0x24}, MasterKeySize))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	const sessionName = "concurrent-session"
	const concurrentCalls = 64

	type result struct {
		material Material
		err      error
	}

	results := make(chan result, concurrentCalls)
	var wg sync.WaitGroup
	for range concurrentCalls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			material, err := manager.GetOrCreate(context.Background(), sessionName)
			results <- result{material: material, err: err}
		}()
	}

	store.waitForGetMiss(t)
	store.releaseBlockedGets()

	wg.Wait()
	close(results)

	var materials []Material
	for result := range results {
		if result.err != nil {
			t.Fatalf("GetOrCreate() error = %v", result.err)
		}
		materials = append(materials, result.material)
	}

	if len(materials) != concurrentCalls {
		t.Fatalf("len(materials) = %d, want %d", len(materials), concurrentCalls)
	}

	if store.putCount() != 1 {
		t.Fatalf("putCount() = %d, want 1", store.putCount())
	}

	saltIDs := make(map[string]struct{}, concurrentCalls)
	salts := make(map[string]struct{}, concurrentCalls)
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

type blockingScrubSaltStore struct {
	mu          sync.Mutex
	record      *store.ScrubSalt
	getMisses   int
	blockedGets chan struct{}
	releaseGets chan struct{}
	puts        int
}

func newBlockingScrubSaltStore(getMisses int) *blockingScrubSaltStore {
	return &blockingScrubSaltStore{
		getMisses:   getMisses,
		blockedGets: make(chan struct{}, getMisses),
		releaseGets: make(chan struct{}),
	}
}

func (s *blockingScrubSaltStore) waitForGetMiss(t *testing.T) {
	t.Helper()

	select {
	case <-s.blockedGets:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked GetScrubSalt miss")
	}
}

func (s *blockingScrubSaltStore) releaseBlockedGets() {
	close(s.releaseGets)
}

func (s *blockingScrubSaltStore) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

func (s *blockingScrubSaltStore) CreateRun(context.Context, store.RunRecord) error {
	return errors.New("unexpected CreateRun call")
}

func (s *blockingScrubSaltStore) GetRunRecord(context.Context, string) (store.RunRecord, error) {
	return store.RunRecord{}, errors.New("unexpected GetRunRecord call")
}

func (s *blockingScrubSaltStore) GetLatestRunRecord(context.Context, string) (store.RunRecord, error) {
	return store.RunRecord{}, errors.New("unexpected GetLatestRunRecord call")
}

func (s *blockingScrubSaltStore) GetRun(context.Context, string) (recorder.Run, error) {
	return recorder.Run{}, errors.New("unexpected GetRun call")
}

func (s *blockingScrubSaltStore) GetLatestRun(context.Context, string) (recorder.Run, error) {
	return recorder.Run{}, errors.New("unexpected GetLatestRun call")
}

func (s *blockingScrubSaltStore) UpdateRun(context.Context, store.RunRecord) error {
	return errors.New("unexpected UpdateRun call")
}

func (s *blockingScrubSaltStore) DeleteRun(context.Context, string) error {
	return errors.New("unexpected DeleteRun call")
}

func (s *blockingScrubSaltStore) DeleteSession(context.Context, string) error {
	return errors.New("unexpected DeleteSession call")
}

func (s *blockingScrubSaltStore) WriteInteraction(context.Context, recorder.Interaction) error {
	return errors.New("unexpected WriteInteraction call")
}

func (s *blockingScrubSaltStore) ListInteractions(context.Context, string) ([]recorder.Interaction, error) {
	return nil, errors.New("unexpected ListInteractions call")
}

func (s *blockingScrubSaltStore) PutScrubSalt(_ context.Context, salt store.ScrubSalt) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	copySalt := salt
	copySalt.SaltEncrypted = append([]byte(nil), salt.SaltEncrypted...)
	s.record = &copySalt
	s.puts++
	return nil
}

func (s *blockingScrubSaltStore) GetScrubSalt(_ context.Context, sessionName string) (store.ScrubSalt, error) {
	s.mu.Lock()
	if s.record != nil && s.record.SessionName == sessionName {
		record := *s.record
		record.SaltEncrypted = append([]byte(nil), s.record.SaltEncrypted...)
		s.mu.Unlock()
		return record, nil
	}

	shouldBlock := s.getMisses > 0
	if shouldBlock {
		s.getMisses--
	}
	s.mu.Unlock()

	if shouldBlock {
		s.blockedGets <- struct{}{}
		<-s.releaseGets
		return store.ScrubSalt{}, store.ErrNotFound
	}

	return store.ScrubSalt{}, store.ErrNotFound
}

func (s *blockingScrubSaltStore) PutBaseline(context.Context, store.Baseline) error {
	return errors.New("unexpected PutBaseline call")
}

func (s *blockingScrubSaltStore) GetBaseline(context.Context, string) (store.Baseline, error) {
	return store.Baseline{}, errors.New("unexpected GetBaseline call")
}

func (s *blockingScrubSaltStore) GetLatestBaseline(context.Context, string) (store.Baseline, error) {
	return store.Baseline{}, errors.New("unexpected GetLatestBaseline call")
}

func (s *blockingScrubSaltStore) Close() error {
	return nil
}
