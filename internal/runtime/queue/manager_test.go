package queue

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

func TestManagerAdvanceTimeDeliversPushEventsInDueOrder(t *testing.T) {
	t.Parallel()

	sqliteStore := openQueueTestStore(t)
	defer sqliteStore.Close()
	manager := newQueueTestManager(t, sqliteStore, fixedQueueTime())

	ctx := context.Background()
	session := createQueueSession(t, sqliteStore, "push-session")
	clock, err := manager.Now(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("Now() error = %v", err)
	}

	first, err := manager.Schedule(ctx, ScheduleOptions{
		SessionName:  session.SessionName,
		Service:      "stripe",
		Topic:        "webhook.invoice.created",
		DeliveryMode: store.ScheduledEventDeliveryModePush,
		DueAt:        clock.CurrentTime.Add(5 * time.Minute),
		Payload:      map[string]any{"invoice_id": "in_001"},
	})
	if err != nil {
		t.Fatalf("Schedule(first) error = %v", err)
	}
	second, err := manager.Schedule(ctx, ScheduleOptions{
		SessionName:  session.SessionName,
		Service:      "stripe",
		Topic:        "webhook.invoice.paid",
		DeliveryMode: store.ScheduledEventDeliveryModePush,
		DueAt:        clock.CurrentTime.Add(10 * time.Minute),
		Payload:      map[string]any{"invoice_id": "in_001"},
	})
	if err != nil {
		t.Fatalf("Schedule(second) error = %v", err)
	}

	result, err := manager.AdvanceTime(ctx, session.SessionName, 4*time.Minute)
	if err != nil {
		t.Fatalf("AdvanceTime(4m) error = %v", err)
	}
	if len(result.DeliveredPush) != 0 {
		t.Fatalf("DeliveredPush after 4m = %#v, want empty", result.DeliveredPush)
	}

	result, err = manager.AdvanceTime(ctx, session.SessionName, time.Minute)
	if err != nil {
		t.Fatalf("AdvanceTime(1m) error = %v", err)
	}
	if len(result.DeliveredPush) != 1 || result.DeliveredPush[0].EventID != first.EventID {
		t.Fatalf("DeliveredPush after 5m = %#v, want first event %q", result.DeliveredPush, first.EventID)
	}

	gotFirst, err := sqliteStore.GetScheduledEvent(ctx, first.EventID)
	if err != nil {
		t.Fatalf("GetScheduledEvent(first) error = %v", err)
	}
	if gotFirst.Status != store.ScheduledEventStatusDelivered {
		t.Fatalf("first status = %q, want delivered", gotFirst.Status)
	}

	result, err = manager.AdvanceTime(ctx, session.SessionName, 5*time.Minute)
	if err != nil {
		t.Fatalf("AdvanceTime(next 5m) error = %v", err)
	}
	if len(result.DeliveredPush) != 1 || result.DeliveredPush[0].EventID != second.EventID {
		t.Fatalf("DeliveredPush after 10m = %#v, want second event %q", result.DeliveredPush, second.EventID)
	}
}

func TestManagerPullDueDeliversOnlyPullEvents(t *testing.T) {
	t.Parallel()

	sqliteStore := openQueueTestStore(t)
	defer sqliteStore.Close()
	manager := newQueueTestManager(t, sqliteStore, fixedQueueTime())

	ctx := context.Background()
	session := createQueueSession(t, sqliteStore, "pull-session")
	clock, err := manager.Now(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("Now() error = %v", err)
	}

	pull, err := manager.Schedule(ctx, ScheduleOptions{
		SessionName:  session.SessionName,
		Service:      "gmail",
		Topic:        "message.available",
		DeliveryMode: store.ScheduledEventDeliveryModePull,
		DueAt:        clock.CurrentTime.Add(2 * time.Minute),
		Payload:      map[string]any{"message_id": "msg_001"},
	})
	if err != nil {
		t.Fatalf("Schedule(pull) error = %v", err)
	}
	_, err = manager.Schedule(ctx, ScheduleOptions{
		SessionName:  session.SessionName,
		Service:      "gmail",
		Topic:        "message.push",
		DeliveryMode: store.ScheduledEventDeliveryModePush,
		DueAt:        clock.CurrentTime.Add(2 * time.Minute),
		Payload:      map[string]any{"message_id": "msg_002"},
	})
	if err != nil {
		t.Fatalf("Schedule(push) error = %v", err)
	}

	result, err := manager.AdvanceTime(ctx, session.SessionName, 2*time.Minute)
	if err != nil {
		t.Fatalf("AdvanceTime() error = %v", err)
	}
	if len(result.DeliveredPush) != 1 {
		t.Fatalf("DeliveredPush = %#v, want one push event", result.DeliveredPush)
	}

	pulled, err := manager.PullDue(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("PullDue() error = %v", err)
	}
	if len(pulled) != 1 || pulled[0].EventID != pull.EventID {
		t.Fatalf("PullDue() = %#v, want pull event %q", pulled, pull.EventID)
	}
	if pulled[0].Status != store.ScheduledEventStatusDelivered || pulled[0].DeliveredAt == nil {
		t.Fatalf("pulled event status = %q delivered_at = %v, want delivered timestamp", pulled[0].Status, pulled[0].DeliveredAt)
	}

	pulledAgain, err := manager.PullDue(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("PullDue(second) error = %v", err)
	}
	if len(pulledAgain) != 0 {
		t.Fatalf("PullDue(second) = %#v, want empty", pulledAgain)
	}
}

func TestManagerRejectsNegativeAdvanceTime(t *testing.T) {
	t.Parallel()

	sqliteStore := openQueueTestStore(t)
	defer sqliteStore.Close()
	manager := newQueueTestManager(t, sqliteStore, fixedQueueTime())

	ctx := context.Background()
	session := createQueueSession(t, sqliteStore, "negative-advance-session")
	clock, err := manager.Now(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("Now() error = %v", err)
	}

	if _, err := manager.AdvanceTime(ctx, session.SessionName, -time.Second); err == nil {
		t.Fatal("AdvanceTime(negative) expected failure")
	}

	gotClock, err := sqliteStore.GetSessionClock(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("GetSessionClock() error = %v", err)
	}
	if !gotClock.CurrentTime.Equal(clock.CurrentTime) {
		t.Fatalf("CurrentTime after rejected advance = %s, want %s", gotClock.CurrentTime, clock.CurrentTime)
	}
}

func TestManagerAdvanceTimeIsAtomicAcrossConcurrentManagers(t *testing.T) {
	t.Parallel()

	sqliteStore := openQueueTestStore(t)
	defer sqliteStore.Close()

	ctx := context.Background()
	session := createQueueSession(t, sqliteStore, "concurrent-advance-session")
	initial := fixedQueueTime()
	managerA := newQueueTestManager(t, sqliteStore, initial)
	managerB := newQueueTestManager(t, sqliteStore, initial)

	clock, err := managerA.Now(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("Now() error = %v", err)
	}

	const advanceCount = 50
	var wg sync.WaitGroup
	errs := make(chan error, advanceCount)
	for idx := 0; idx < advanceCount; idx++ {
		manager := managerA
		if idx%2 == 1 {
			manager = managerB
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := manager.AdvanceTime(ctx, session.SessionName, time.Second)
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("AdvanceTime() concurrent error = %v", err)
	}

	gotClock, err := sqliteStore.GetSessionClock(ctx, session.SessionName)
	if err != nil {
		t.Fatalf("GetSessionClock() error = %v", err)
	}
	want := clock.CurrentTime.Add(advanceCount * time.Second)
	if !gotClock.CurrentTime.Equal(want) {
		t.Fatalf("CurrentTime after concurrent advances = %s, want %s", gotClock.CurrentTime, want)
	}
}

func TestManagerRequiresExistingSession(t *testing.T) {
	t.Parallel()

	sqliteStore := openQueueTestStore(t)
	defer sqliteStore.Close()
	manager := newQueueTestManager(t, sqliteStore, fixedQueueTime())

	if _, err := manager.Now(context.Background(), "missing-session"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Now(missing) error = %v, want store.ErrNotFound", err)
	}
}

func openQueueTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()

	sqliteStore, err := sqlitestore.OpenStore(context.Background(), filepath.Join(t.TempDir(), "stagehand.db"))
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}

	return sqliteStore
}

func newQueueTestManager(t *testing.T, eventStore store.EventQueueStore, now time.Time) *Manager {
	t.Helper()

	nextID := 0
	manager, err := NewManager(
		eventStore,
		WithClock(func() time.Time {
			return now
		}),
		WithIDGenerator(func(prefix string) (string, error) {
			nextID++
			return prefix + "_queue_test_" + time.Unix(int64(nextID), 0).UTC().Format("20060102150405"), nil
		}),
	)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	return manager
}

func createQueueSession(t *testing.T, sqliteStore *sqlitestore.Store, sessionName string) store.SessionRecord {
	t.Helper()

	now := fixedQueueTime()
	session := store.SessionRecord{
		SessionName: sessionName,
		Status:      store.SessionStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := sqliteStore.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("CreateSession(%q) error = %v", sessionName, err)
	}

	return session
}

func fixedQueueTime() time.Time {
	return time.Date(2026, time.April, 24, 15, 0, 0, 0, time.UTC)
}
