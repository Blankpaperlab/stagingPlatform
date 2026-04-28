package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stagehand/internal/store"
)

type Manager struct {
	store store.SessionStore
	now   func() time.Time
	newID func(prefix string) (string, error)
}

type Option func(*Manager)

func WithClock(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

func WithIDGenerator(newID func(prefix string) (string, error)) Option {
	return func(m *Manager) {
		if newID != nil {
			m.newID = newID
		}
	}
}

func NewManager(sessionStore store.SessionStore, opts ...Option) (*Manager, error) {
	if sessionStore == nil {
		return nil, fmt.Errorf("session store is required")
	}

	manager := &Manager{
		store: sessionStore,
		now: func() time.Time {
			return time.Now().UTC()
		},
		newID: randomID,
	}

	for _, opt := range opts {
		opt(manager)
	}

	return manager, nil
}

func (m *Manager) Create(ctx context.Context, sessionName string) (store.SessionRecord, error) {
	now := m.now()
	session := store.SessionRecord{
		SessionName: strings.TrimSpace(sessionName),
		Status:      store.SessionStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := m.store.CreateSession(ctx, session); err != nil {
		return store.SessionRecord{}, err
	}

	return session, nil
}

func (m *Manager) Fork(ctx context.Context, parentSessionName string, childSessionName string) (store.SessionRecord, error) {
	parent, err := m.store.GetSession(ctx, strings.TrimSpace(parentSessionName))
	if err != nil {
		return store.SessionRecord{}, err
	}

	now := m.now()
	child := store.SessionRecord{
		SessionName:       strings.TrimSpace(childSessionName),
		ParentSessionName: parent.SessionName,
		Status:            store.SessionStatusActive,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := m.store.CreateSession(ctx, child); err != nil {
		return store.SessionRecord{}, err
	}

	if strings.TrimSpace(parent.CurrentSnapshotID) == "" {
		return child, nil
	}

	parentSnapshot, err := m.store.GetSessionSnapshot(ctx, parent.CurrentSnapshotID)
	if err != nil {
		_ = m.store.DeleteSession(ctx, child.SessionName)
		return store.SessionRecord{}, err
	}

	snapshotID, err := m.newID("snap")
	if err != nil {
		_ = m.store.DeleteSession(ctx, child.SessionName)
		return store.SessionRecord{}, err
	}

	childState, err := cloneState(parentSnapshot.State)
	if err != nil {
		_ = m.store.DeleteSession(ctx, child.SessionName)
		return store.SessionRecord{}, err
	}

	childSnapshot := store.SessionSnapshot{
		SnapshotID:       snapshotID,
		SessionName:      child.SessionName,
		ParentSnapshotID: parentSnapshot.SnapshotID,
		SourceRunID:      parentSnapshot.SourceRunID,
		State:            childState,
		CreatedAt:        now,
	}
	if err := m.store.PutSessionSnapshot(ctx, childSnapshot); err != nil {
		_ = m.store.DeleteSession(ctx, child.SessionName)
		return store.SessionRecord{}, err
	}

	child.CurrentSnapshotID = childSnapshot.SnapshotID
	if err := m.store.UpdateSession(ctx, child); err != nil {
		_ = m.store.DeleteSession(ctx, child.SessionName)
		return store.SessionRecord{}, err
	}

	return child, nil
}

func (m *Manager) Snapshot(ctx context.Context, sessionName string, state map[string]any) (store.SessionSnapshot, error) {
	snapshotID, err := m.newID("snap")
	if err != nil {
		return store.SessionSnapshot{}, err
	}

	now := m.now()
	clonedState, err := cloneState(state)
	if err != nil {
		return store.SessionSnapshot{}, err
	}

	snapshot := store.SessionSnapshot{
		SnapshotID:  snapshotID,
		SessionName: strings.TrimSpace(sessionName),
		State:       clonedState,
		CreatedAt:   now,
	}
	return m.store.AppendSessionSnapshot(ctx, snapshot, now)
}

func (m *Manager) Restore(ctx context.Context, sessionName string, snapshotID string) (store.SessionRecord, error) {
	session, err := m.store.GetSession(ctx, strings.TrimSpace(sessionName))
	if err != nil {
		return store.SessionRecord{}, err
	}

	snapshot, err := m.store.GetSessionSnapshot(ctx, strings.TrimSpace(snapshotID))
	if err != nil {
		return store.SessionRecord{}, err
	}
	if snapshot.SessionName != session.SessionName {
		return store.SessionRecord{}, fmt.Errorf("snapshot %q belongs to session %q, not %q", snapshot.SnapshotID, snapshot.SessionName, session.SessionName)
	}

	session.CurrentSnapshotID = snapshot.SnapshotID
	session.UpdatedAt = m.now()
	if err := m.store.UpdateSession(ctx, session); err != nil {
		return store.SessionRecord{}, err
	}

	return session, nil
}

func (m *Manager) CurrentSnapshot(ctx context.Context, sessionName string) (store.SessionSnapshot, error) {
	session, err := m.store.GetSession(ctx, strings.TrimSpace(sessionName))
	if err != nil {
		return store.SessionSnapshot{}, err
	}
	if strings.TrimSpace(session.CurrentSnapshotID) == "" {
		return store.SessionSnapshot{}, fmt.Errorf("current snapshot for session %q: %w", session.SessionName, store.ErrNotFound)
	}
	return m.store.GetSessionSnapshot(ctx, session.CurrentSnapshotID)
}

func (m *Manager) Destroy(ctx context.Context, sessionName string) error {
	return m.store.DeleteSession(ctx, strings.TrimSpace(sessionName))
}

func cloneState(state map[string]any) (map[string]any, error) {
	if state == nil {
		return map[string]any{}, nil
	}

	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("clone session state: %w", err)
	}

	var cloned map[string]any
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return nil, fmt.Errorf("clone session state: %w", err)
	}

	return cloned, nil
}

func randomID(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}
