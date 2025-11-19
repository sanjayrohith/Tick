package materializer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// fakeStore is an in-memory Store double for exercising the run loop without
// a database.
type fakeStore struct {
	schedules []*domain.Schedule

	enqueueBulkFunc func(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error)

	advanced map[int64]time.Time
}

func newFakeStore(schedules ...*domain.Schedule) *fakeStore {
	return &fakeStore{schedules: schedules, advanced: map[int64]time.Time{}}
}

func (f *fakeStore) ListEnabledSchedules(_ context.Context) ([]*domain.Schedule, error) {
	var out []*domain.Schedule
	for _, s := range f.schedules {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeStore) EnqueueBulk(ctx context.Context, tasks []*domain.Task) ([]*domain.Task, error) {
	if f.enqueueBulkFunc != nil {
		return f.enqueueBulkFunc(ctx, tasks)
	}
	return tasks, nil
}

func (f *fakeStore) AdvanceLastMaterializedAt(_ context.Context, scheduleID int64, at time.Time) error {
	f.advanced[scheduleID] = at
	return nil
}

func TestRunOnceMaterializesEveryEnabledSchedule(t *testing.T) {
	s1 := &domain.Schedule{ID: 1, Cron: "0 0 * * *", Timezone: "UTC", Enabled: true}
	s2 := &domain.Schedule{ID: 2, Cron: "0 12 * * *", Timezone: "UTC", Enabled: true}
	disabled := &domain.Schedule{ID: 3, Cron: "0 0 * * *", Timezone: "UTC", Enabled: false}
	store := newFakeStore(s1, s2, disabled)

	New(store, 24*time.Hour, time.Hour, nil).RunOnce(context.Background())

	if _, ok := store.advanced[1]; !ok {
		t.Error("schedule 1 was not advanced")
	}
	if _, ok := store.advanced[2]; !ok {
		t.Error("schedule 2 was not advanced")
	}
	if _, ok := store.advanced[3]; ok {
		t.Error("a disabled schedule must never be materialized")
	}
}

func TestRunOnceIsolatesAFailingSchedule(t *testing.T) {
	bad := &domain.Schedule{ID: 1, Cron: "not a cron expr", Enabled: true}
	good := &domain.Schedule{ID: 2, Cron: "0 0 * * *", Timezone: "UTC", Enabled: true}
	store := newFakeStore(bad, good)

	New(store, 24*time.Hour, time.Hour, nil).RunOnce(context.Background())

	if _, ok := store.advanced[1]; ok {
		t.Error("a schedule with an invalid cron expression must not be advanced")
	}
	if _, ok := store.advanced[2]; !ok {
		t.Error("a good schedule must still be materialized after a bad one earlier in the list")
	}
	if !good.Enabled {
		t.Error("materialization must never disable a schedule on its own initiative")
	}
	if !bad.Enabled {
		t.Error("the failing schedule must also be left enabled")
	}
}

func TestMaterializeOneAdvancesOnlyAfterSuccessfulInsert(t *testing.T) {
	s := &domain.Schedule{ID: 1, Cron: "0 0 * * *", Timezone: "UTC", Enabled: true}
	store := newFakeStore(s)
	store.enqueueBulkFunc = func(_ context.Context, _ []*domain.Task) ([]*domain.Task, error) {
		return nil, errors.New("database is down")
	}

	r := New(store, 24*time.Hour, time.Hour, nil)
	err := r.materializeOne(context.Background(), time.Date(2025, 11, 19, 0, 0, 0, 0, time.UTC), s)
	if err == nil {
		t.Fatal("materializeOne() succeeded, want the insert failure to propagate")
	}
	if _, ok := store.advanced[1]; ok {
		t.Error("last_materialized_at was advanced despite the insert failing")
	}
}

func TestRunOnceWithNoDueFireTimesDoesNothing(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	s := &domain.Schedule{
		ID: 1, Cron: "0 0 * * *", Timezone: "UTC", Enabled: true,
		LastMaterializedAt: &future,
	}
	store := newFakeStore(s)

	New(store, time.Minute, time.Hour, nil).RunOnce(context.Background())

	if _, ok := store.advanced[1]; ok {
		t.Error("a schedule with nothing due within the horizon must not be advanced")
	}
}
