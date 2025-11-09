package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// TestClaimOrdersByPriorityThenRunAt seeds mixed priorities and run_at values
// and asserts claim returns them in exactly the order the PRD promises:
// highest priority first, ties broken by the earliest run_at.
func TestClaimOrdersByPriorityThenRunAt(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().Add(-time.Minute)

	// Seeded out of order on purpose: insertion order must not leak into
	// claim order.
	seeds := []struct {
		name     string
		priority int
		runAt    time.Time
	}{
		{"low-later", 0, now.Add(3 * time.Second)},
		{"high-later", 10, now.Add(2 * time.Second)},
		{"high-earlier", 10, now.Add(1 * time.Second)},
		{"low-earlier", 0, now},
		{"mid", 5, now.Add(time.Second)},
	}

	byName := make(map[string]int64, len(seeds))
	for _, sd := range seeds {
		inserted, err := s.Enqueue(ctx, &domain.Task{
			Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
			Priority: sd.priority, RunAt: sd.runAt,
		})
		if err != nil {
			t.Fatalf("Enqueue(%s) = %v, want success", sd.name, err)
		}
		byName[sd.name] = inserted.ID
	}

	claimed, err := s.Claim(ctx, "default", "worker-1", len(seeds))
	if err != nil {
		t.Fatalf("Claim() = %v, want success", err)
	}

	wantOrder := []string{"high-earlier", "high-later", "mid", "low-earlier", "low-later"}
	if len(claimed) != len(wantOrder) {
		t.Fatalf("len(claimed) = %d, want %d", len(claimed), len(wantOrder))
	}
	for i, name := range wantOrder {
		if claimed[i].ID != byName[name] {
			t.Errorf("claimed[%d] = task %q (id %d), want %q (id %d)",
				i, idToName(byName, claimed[i].ID), claimed[i].ID, name, byName[name])
		}
	}
}

func idToName(byName map[string]int64, id int64) string {
	for name, taskID := range byName {
		if taskID == id {
			return name
		}
	}
	return "unknown"
}
