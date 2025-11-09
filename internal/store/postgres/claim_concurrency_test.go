package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

// TestConcurrentClaimersNeverObserveTheSameTaskTwice is the test the whole
// project exists to pass: FOR UPDATE SKIP LOCKED must let N workers claim
// concurrently from the same backlog without ever handing two of them the
// same row.
func TestConcurrentClaimersNeverObserveTheSameTaskTwice(t *testing.T) {
	const (
		workers   = 8
		taskCount = 200
		perWorker = taskCount / workers
	)

	s := newStore(t)
	ctx := context.Background()

	seeds := make([]*domain.Task, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		seeds = append(seeds, &domain.Task{
			Queue: "default", Handler: "send_email", Payload: json.RawMessage(`{}`),
		})
	}
	if _, err := s.EnqueueBulk(ctx, seeds); err != nil {
		t.Fatalf("EnqueueBulk() = %v, want success", err)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = make([]int64, 0, taskCount)
	)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			got, err := s.Claim(ctx, "default", workerID, perWorker)
			if err != nil {
				t.Errorf("Claim(%s) = %v, want success", workerID, err)
				return
			}
			ids := make([]int64, len(got))
			for i, task := range got {
				ids[i] = task.ID
			}
			mu.Lock()
			claimed = append(claimed, ids...)
			mu.Unlock()
		}(fmt.Sprintf("worker-%d", w))
	}
	wg.Wait()

	if len(claimed) != taskCount {
		t.Fatalf("total claimed = %d, want %d", len(claimed), taskCount)
	}

	seen := make(map[int64]bool, taskCount)
	for _, id := range claimed {
		if seen[id] {
			t.Errorf("task %d was claimed by more than one worker", id)
		}
		seen[id] = true
	}
	if len(seen) != taskCount {
		t.Errorf("distinct claimed ids = %d, want %d", len(seen), taskCount)
	}
}
