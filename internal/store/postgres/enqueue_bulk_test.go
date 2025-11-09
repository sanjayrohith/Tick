package postgres

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestEnqueueBulkReturnsTasksInSubmissionOrder(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	tasks := make([]*domain.Task, 0, 5)
	for i := 0; i < 5; i++ {
		tasks = append(tasks, &domain.Task{
			Queue:   "default",
			Handler: "send_email",
			Payload: json.RawMessage(`{"n":` + string(rune('0'+i)) + `}`),
		})
	}

	inserted, err := s.EnqueueBulk(ctx, tasks)
	if err != nil {
		t.Fatalf("EnqueueBulk() = %v, want success", err)
	}
	if len(inserted) != len(tasks) {
		t.Fatalf("len(inserted) = %d, want %d", len(inserted), len(tasks))
	}
	for i := 1; i < len(inserted); i++ {
		if inserted[i].ID <= inserted[i-1].ID {
			t.Errorf("inserted[%d].ID = %d, want greater than inserted[%d].ID = %d (submission order)",
				i, inserted[i].ID, i-1, inserted[i-1].ID)
		}
	}
}

func TestEnqueueBulkEmptyInputReturnsNoRows(t *testing.T) {
	s := newStore(t)
	inserted, err := s.EnqueueBulk(context.Background(), nil)
	if err != nil {
		t.Fatalf("EnqueueBulk(nil) = %v, want success", err)
	}
	if len(inserted) != 0 {
		t.Errorf("len(inserted) = %d, want 0", len(inserted))
	}
}
