package worker

import (
	"context"
	"sync"
	"time"
)

// inFlightSet tracks the cancellation function for every task currently
// executing, keyed by task ID, so the heartbeat loop can cancel a dispatch
// whose claim has been lost out from under it.
type inFlightSet struct {
	mu     sync.Mutex
	cancel map[int64]context.CancelFunc
}

func newInFlightSet() *inFlightSet {
	return &inFlightSet{cancel: make(map[int64]context.CancelFunc)}
}

func (s *inFlightSet) add(id int64, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel[id] = cancel
}

func (s *inFlightSet) remove(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancel, id)
}

func (s *inFlightSet) ids() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int64, 0, len(s.cancel))
	for id := range s.cancel {
		ids = append(ids, id)
	}
	return ids
}

func (s *inFlightSet) cancelFunc(id int64) (context.CancelFunc, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cancel[id]
	return c, ok
}

// heartbeatLoop refreshes heartbeat_at for every in-flight task on
// cfg.HeartbeatInterval, in one batched call, until ctx is cancelled.
func (w *Worker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.heartbeatOnce(ctx)
		}
	}
}

// heartbeatOnce sends one batched heartbeat for every in-flight task. When
// the store refreshes fewer rows than were sent, at least one claim was
// lost -- almost always because this worker stalled past the heartbeat TTL
// and the sweeper reclaimed the row. The batch call cannot say which task, so
// each in-flight task is checked individually against the store and the ones
// no longer owned by this worker have their dispatch context cancelled, so a
// handler racing a reclaimed row stops instead of finishing work another
// worker may already be redoing.
func (w *Worker) heartbeatOnce(ctx context.Context) {
	ids := w.inFlight.ids()
	if len(ids) == 0 {
		return
	}

	refreshed, err := w.store.Heartbeat(ctx, ids, w.id)
	if err != nil {
		w.log.ErrorContext(ctx, "heartbeat failed", "error", err)
		return
	}
	if refreshed == len(ids) {
		return
	}

	for _, id := range ids {
		t, err := w.store.Get(ctx, id)
		if err != nil {
			continue
		}
		if t.ClaimedBy != nil && *t.ClaimedBy == w.id {
			continue
		}
		if cancel, ok := w.inFlight.cancelFunc(id); ok {
			cancel()
		}
	}
}
