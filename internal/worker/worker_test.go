package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// fakeStore is a minimal in-memory Store good enough to drive a Worker in
// tests without a real database. It deliberately skips everything the real
// backends are tested for separately -- priority ordering, run_at gating,
// SKIP LOCKED concurrency -- and keeps only what dispatch, retry, and
// completion need: one canonical record per task ID, mutated in place.
type fakeStore struct {
	mu      sync.Mutex
	byID    map[int64]*domain.Task
	pending []int64
	nextID  int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{byID: make(map[int64]*domain.Task)}
}

// enqueue adds t directly to pending, assigning it an ID the way a real
// Enqueuer would.
func (s *fakeStore) enqueue(t domain.Task) *domain.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	t.ID = s.nextID
	t.Status = domain.StatusPending
	s.byID[t.ID] = &t
	s.pending = append(s.pending, t.ID)
	out := t
	return &out
}

func (s *fakeStore) Claim(_ context.Context, _, workerID string, limit int) ([]*domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var claimed []*domain.Task
	var rest []int64
	for _, id := range s.pending {
		if len(claimed) >= limit {
			rest = append(rest, id)
			continue
		}
		t := s.byID[id]
		claimedBy := workerID
		t.Status = domain.StatusRunning
		t.ClaimedBy = &claimedBy
		t.Attempts++
		out := *t
		claimed = append(claimed, &out)
	}
	s.pending = rest
	return claimed, nil
}

func (s *fakeStore) Heartbeat(_ context.Context, ids []int64, workerID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, id := range ids {
		t, ok := s.byID[id]
		if ok && t.Status == domain.StatusRunning && t.ClaimedBy != nil && *t.ClaimedBy == workerID {
			n++
		}
	}
	return n, nil
}

func (s *fakeStore) Complete(_ context.Context, id int64, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok || t.ClaimedBy == nil || *t.ClaimedBy != workerID {
		return domain.ErrClaimLost
	}
	t.Status = domain.StatusSucceeded
	t.ClaimedBy = nil
	return nil
}

func (s *fakeStore) Fail(_ context.Context, id int64, workerID string, execErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok || t.ClaimedBy == nil || *t.ClaimedBy != workerID {
		return domain.ErrClaimLost
	}
	msg := ""
	if execErr != nil {
		msg = execErr.Error()
	}
	t.LastError = &msg
	t.ClaimedBy = nil
	if t.Attempts >= t.MaxAttempts {
		t.Status = domain.StatusDead
		return nil
	}
	t.Status = domain.StatusPending
	s.pending = append(s.pending, id)
	return nil
}

func (s *fakeStore) Get(_ context.Context, id int64) (*domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := *t
	return &out, nil
}

// waitForTerminal polls s for id to reach a terminal status, failing the test
// if it does not within a few seconds.
func waitForTerminal(t *testing.T, s *fakeStore, id int64) *domain.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := s.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}
		if got.Status.Terminal() {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("task %d did not reach a terminal status in time", id)
	return nil
}

func testConfig() Config {
	return Config{
		Queue:             "default",
		ClaimBatch:        10,
		Concurrency:       3,
		HeartbeatInterval: time.Hour,
		TaskTimeout:       2 * time.Second,
		ShutdownGrace:     time.Second,
	}
}

func runAndStop(t *testing.T, w *Worker) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return cancel
}

func TestSuccessPathMarksSucceeded(t *testing.T) {
	store := newFakeStore()
	registry := NewRegistry()
	registry.Register("noop", HandlerFunc(func(context.Context, domain.Task) error {
		return nil
	}))

	task := store.enqueue(domain.Task{Queue: "default", Handler: "noop", MaxAttempts: 3})

	w := New(store, registry, "test-worker", testConfig(), nil)
	runAndStop(t, w)

	got := waitForTerminal(t, store, task.ID)
	if got.Status != domain.StatusSucceeded {
		t.Errorf("status = %q, want %q", got.Status, domain.StatusSucceeded)
	}
	if got.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", got.Attempts)
	}
}

func TestRetryUntilSuccessAndExhaustionIntoDead(t *testing.T) {
	store := newFakeStore()
	registry := NewRegistry()

	// Fails on its first two attempts, then succeeds on the third. Deciding
	// off t.Attempts -- incremented by Claim exactly like the real claim
	// query -- rather than a shared counter keeps this correct even though
	// the worker may run several tasks concurrently.
	registry.Register("flaky", HandlerFunc(func(_ context.Context, task domain.Task) error {
		if task.Attempts < 3 {
			return errors.New("not yet")
		}
		return nil
	}))
	registry.Register("always-fails", HandlerFunc(func(context.Context, domain.Task) error {
		return errors.New("permanent failure")
	}))

	flaky := store.enqueue(domain.Task{Queue: "default", Handler: "flaky", MaxAttempts: 5})
	doomed := store.enqueue(domain.Task{Queue: "default", Handler: "always-fails", MaxAttempts: 2})

	w := New(store, registry, "test-worker", testConfig(), nil)
	runAndStop(t, w)

	gotFlaky := waitForTerminal(t, store, flaky.ID)
	if gotFlaky.Status != domain.StatusSucceeded {
		t.Errorf("flaky task status = %q, want %q", gotFlaky.Status, domain.StatusSucceeded)
	}
	if gotFlaky.Attempts != 3 {
		t.Errorf("flaky task attempts = %d, want 3", gotFlaky.Attempts)
	}

	gotDoomed := waitForTerminal(t, store, doomed.ID)
	if gotDoomed.Status != domain.StatusDead {
		t.Errorf("doomed task status = %q, want %q", gotDoomed.Status, domain.StatusDead)
	}
	if gotDoomed.Attempts != gotDoomed.MaxAttempts {
		t.Errorf("doomed task attempts = %d, want max_attempts %d", gotDoomed.Attempts, gotDoomed.MaxAttempts)
	}
}
