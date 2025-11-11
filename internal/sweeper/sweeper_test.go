package sweeper

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeMetrics struct {
	orphansRecovered atomic.Int64
}

func (m *fakeMetrics) OrphansRecovered(n int) { m.orphansRecovered.Add(int64(n)) }

type fakeRecoverer struct {
	calls   atomic.Int64
	failing atomic.Bool
}

func (f *fakeRecoverer) RecoverOrphans(context.Context) (int, error) {
	f.calls.Add(1)
	if f.failing.Load() {
		return 0, errors.New("database unreachable")
	}
	return 1, nil
}

func TestRunSweepsRepeatedlyUntilCancelled(t *testing.T) {
	rec := &fakeRecoverer{}
	metrics := &fakeMetrics{}
	s := New(rec, 5*time.Millisecond, nil, metrics)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	s.Run(ctx)

	if rec.calls.Load() < 3 {
		t.Errorf("calls = %d, want at least 3 sweeps in 60ms on a 5ms interval", rec.calls.Load())
	}
	if metrics.orphansRecovered.Load() != rec.calls.Load() {
		t.Errorf("orphansRecovered = %d, want one recorded per sweep (%d)",
			metrics.orphansRecovered.Load(), rec.calls.Load())
	}
}

func TestRunSurvivesRecoveryErrorsAndKeepsGoing(t *testing.T) {
	rec := &fakeRecoverer{}
	rec.failing.Store(true)
	s := New(rec, 5*time.Millisecond, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	s.Run(ctx)

	if rec.calls.Load() < 2 {
		t.Errorf("calls = %d, want the loop to keep sweeping despite errors", rec.calls.Load())
	}
}

func TestRunReturnsPromptlyWhenContextIsAlreadyCancelled(t *testing.T) {
	rec := &fakeRecoverer{}
	s := New(rec, time.Hour, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not return promptly after context cancellation")
	}
}
