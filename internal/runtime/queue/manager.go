package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"stagehand/internal/store"
)

type Manager struct {
	store store.EventQueueStore
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

type ScheduleOptions struct {
	SessionName  string
	Service      string
	Topic        string
	DeliveryMode store.ScheduledEventDeliveryMode
	DueAt        time.Time
	Payload      map[string]any
}

type AdvanceResult struct {
	Clock         store.SessionClock
	DeliveredPush []store.ScheduledEvent
}

func NewManager(eventStore store.EventQueueStore, opts ...Option) (*Manager, error) {
	if eventStore == nil {
		return nil, fmt.Errorf("event queue store is required")
	}

	manager := &Manager{
		store: eventStore,
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

func (m *Manager) Schedule(ctx context.Context, opts ScheduleOptions) (store.ScheduledEvent, error) {
	eventID, err := m.newID("evt")
	if err != nil {
		return store.ScheduledEvent{}, err
	}

	payload := opts.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	now := m.now()
	event := store.ScheduledEvent{
		EventID:      eventID,
		SessionName:  strings.TrimSpace(opts.SessionName),
		Service:      strings.TrimSpace(opts.Service),
		Topic:        strings.TrimSpace(opts.Topic),
		DeliveryMode: opts.DeliveryMode,
		DueAt:        opts.DueAt,
		Payload:      payload,
		Status:       store.ScheduledEventStatusPending,
		CreatedAt:    now,
	}

	if err := m.store.PutScheduledEvent(ctx, event); err != nil {
		return store.ScheduledEvent{}, err
	}

	return event, nil
}

func (m *Manager) Now(ctx context.Context, sessionName string) (store.SessionClock, error) {
	return m.ensureClock(ctx, strings.TrimSpace(sessionName))
}

func (m *Manager) AdvanceTime(ctx context.Context, sessionName string, by time.Duration) (AdvanceResult, error) {
	if by < 0 {
		return AdvanceResult{}, fmt.Errorf("advance_time duration cannot be negative")
	}

	sessionName = strings.TrimSpace(sessionName)
	clock, err := m.ensureClock(ctx, sessionName)
	if err != nil {
		return AdvanceResult{}, err
	}

	clock.CurrentTime = clock.CurrentTime.Add(by)
	clock.UpdatedAt = m.now()
	if err := m.store.PutSessionClock(ctx, clock); err != nil {
		return AdvanceResult{}, err
	}

	duePush, err := m.store.ListDueScheduledEvents(ctx, sessionName, store.ScheduledEventDeliveryModePush, clock.CurrentTime)
	if err != nil {
		return AdvanceResult{}, err
	}

	if err := m.markDelivered(ctx, sessionName, duePush, clock.CurrentTime); err != nil {
		return AdvanceResult{}, err
	}

	return AdvanceResult{
		Clock:         clock,
		DeliveredPush: withDeliveredAt(duePush, clock.CurrentTime),
	}, nil
}

func (m *Manager) PullDue(ctx context.Context, sessionName string) ([]store.ScheduledEvent, error) {
	sessionName = strings.TrimSpace(sessionName)
	clock, err := m.ensureClock(ctx, sessionName)
	if err != nil {
		return nil, err
	}

	duePull, err := m.store.ListDueScheduledEvents(ctx, sessionName, store.ScheduledEventDeliveryModePull, clock.CurrentTime)
	if err != nil {
		return nil, err
	}

	if err := m.markDelivered(ctx, sessionName, duePull, clock.CurrentTime); err != nil {
		return nil, err
	}

	return withDeliveredAt(duePull, clock.CurrentTime), nil
}

func (m *Manager) ensureClock(ctx context.Context, sessionName string) (store.SessionClock, error) {
	clock, err := m.store.GetSessionClock(ctx, sessionName)
	if err == nil {
		return clock, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.SessionClock{}, err
	}

	now := m.now()
	clock = store.SessionClock{
		SessionName: sessionName,
		CurrentTime: now,
		UpdatedAt:   now,
	}
	if err := m.store.PutSessionClock(ctx, clock); err != nil {
		return store.SessionClock{}, err
	}

	return clock, nil
}

func (m *Manager) markDelivered(
	ctx context.Context,
	sessionName string,
	events []store.ScheduledEvent,
	deliveredAt time.Time,
) error {
	if len(events) == 0 {
		return nil
	}

	eventIDs := make([]string, 0, len(events))
	for _, event := range events {
		eventIDs = append(eventIDs, event.EventID)
	}

	return m.store.MarkScheduledEventsDelivered(ctx, sessionName, eventIDs, deliveredAt)
}

func withDeliveredAt(events []store.ScheduledEvent, deliveredAt time.Time) []store.ScheduledEvent {
	delivered := make([]store.ScheduledEvent, 0, len(events))
	for _, event := range events {
		event.Status = store.ScheduledEventStatusDelivered
		event.DeliveredAt = &deliveredAt
		delivered = append(delivered, event)
	}
	return delivered
}

func randomID(prefix string) (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}
