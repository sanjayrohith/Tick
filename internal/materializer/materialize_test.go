package materializer

import (
	"context"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// fakeEnqueuer is an in-memory Enqueuer double, recording exactly what was
// submitted so Materialize's task-building can be checked without a
// database.
type fakeEnqueuer struct {
	submitted []*domain.Task
}

func (f *fakeEnqueuer) EnqueueBulk(_ context.Context, tasks []*domain.Task) ([]*domain.Task, error) {
	f.submitted = tasks
	out := make([]*domain.Task, len(tasks))
	copy(out, tasks)
	return out, nil
}

func TestMaterializeBuildsOneTaskPerFireTime(t *testing.T) {
	s := &domain.Schedule{
		ID:          7,
		Queue:       "emails",
		Handler:     "send_digest",
		MaxAttempts: 3,
	}
	fireTimes := []time.Time{
		time.Date(2025, 11, 19, 9, 0, 0, 0, time.UTC),
		time.Date(2025, 11, 20, 9, 0, 0, 0, time.UTC),
	}

	store := &fakeEnqueuer{}
	got, err := Materialize(context.Background(), store, s, fireTimes)
	if err != nil {
		t.Fatalf("Materialize() = %v, want success", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}

	for i, ft := range fireTimes {
		task := store.submitted[i]
		if task.Queue != "emails" || task.Handler != "send_digest" {
			t.Errorf("task[%d] queue/handler = %q/%q, want emails/send_digest", i, task.Queue, task.Handler)
		}
		if !task.RunAt.Equal(ft) {
			t.Errorf("task[%d].RunAt = %v, want %v", i, task.RunAt, ft)
		}
		want := domain.IdempotencyKeyFor(7, ft)
		if task.IdempotencyKey == nil || *task.IdempotencyKey != want {
			t.Errorf("task[%d].IdempotencyKey = %v, want %q", i, task.IdempotencyKey, want)
		}
		if task.ScheduleID == nil || *task.ScheduleID != 7 {
			t.Errorf("task[%d].ScheduleID = %v, want 7", i, task.ScheduleID)
		}
	}
}

func TestMaterializeWithNoFireTimesDoesNotCallStore(t *testing.T) {
	s := &domain.Schedule{ID: 1}
	store := &fakeEnqueuer{}

	got, err := Materialize(context.Background(), store, s, nil)
	if err != nil {
		t.Fatalf("Materialize() = %v, want success", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
	if store.submitted != nil {
		t.Error("EnqueueBulk was called with no fire times to materialize")
	}
}
