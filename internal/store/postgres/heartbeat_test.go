package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestHeartbeatRefreshesOwnedRunningTasks(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.EnqueueBulk(ctx, []*domain.Task{
		{Queue: "default", Handler: "h", Payload: json.RawMessage(`{}`)},
		{Queue: "default", Handler: "h", Payload: json.RawMessage(`{}`)},
	}); err != nil {
		t.Fatalf("EnqueueBulk() = %v, want success", err)
	}
	claimed, err := s.Claim(ctx, "default", "worker-1", 2)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("Claim() = (%v, %v), want two claimed tasks", claimed, err)
	}

	before, err := scanTask(testPool.QueryRow(ctx,
		"SELECT "+taskColumns+" FROM tasks WHERE id = $1", claimed[0].ID))
	if err != nil {
		t.Fatalf("reading task before heartbeat: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	ids := []int64{claimed[0].ID, claimed[1].ID}
	refreshed, err := s.Heartbeat(ctx, ids, "worker-1")
	if err != nil {
		t.Fatalf("Heartbeat() = %v, want success", err)
	}
	if refreshed != 2 {
		t.Errorf("refreshed = %d, want 2", refreshed)
	}

	after, err := scanTask(testPool.QueryRow(ctx,
		"SELECT "+taskColumns+" FROM tasks WHERE id = $1", claimed[0].ID))
	if err != nil {
		t.Fatalf("reading task after heartbeat: %v", err)
	}
	if !after.HeartbeatAt.After(*before.HeartbeatAt) {
		t.Errorf("HeartbeatAt did not advance: before=%v after=%v", before.HeartbeatAt, after.HeartbeatAt)
	}
}

func TestHeartbeatDoesNotRefreshTasksClaimedByAnotherWorker(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	if _, err := s.Enqueue(ctx, &domain.Task{
		Queue: "default", Handler: "h", Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("Enqueue() = %v, want success", err)
	}
	claimed, err := s.Claim(ctx, "default", "worker-1", 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("Claim() = (%v, %v), want one claimed task", claimed, err)
	}

	refreshed, err := s.Heartbeat(ctx, []int64{claimed[0].ID}, "worker-2")
	if err != nil {
		t.Fatalf("Heartbeat() = %v, want success", err)
	}
	if refreshed != 0 {
		t.Errorf("refreshed = %d, want 0: worker-2 does not own this claim", refreshed)
	}
}

func TestHeartbeatEmptyIDsIsANoop(t *testing.T) {
	s := newStore(t)
	refreshed, err := s.Heartbeat(context.Background(), nil, "worker-1")
	if err != nil {
		t.Fatalf("Heartbeat(nil) = %v, want success", err)
	}
	if refreshed != 0 {
		t.Errorf("refreshed = %d, want 0", refreshed)
	}
}
