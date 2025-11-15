package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// noopHandler succeeds immediately. It exists to exercise claim-to-completion
// latency without any handler-side cost of its own, which is what the
// benchmarks use it for.
func noopHandler(context.Context, domain.Task) error {
	return nil
}

// sleepPayload is the payload sleepHandler understands: {"duration_ms": 100}.
type sleepPayload struct {
	DurationMS int `json:"duration_ms"`
}

// sleepHandler sleeps for the payload's duration, or 100ms with no payload,
// honouring context cancellation so a shutdown or a lost claim interrupts it
// promptly instead of running to completion regardless.
func sleepHandler(ctx context.Context, t domain.Task) error {
	d := 100 * time.Millisecond
	var p sleepPayload
	if len(t.Payload) > 0 {
		if err := json.Unmarshal(t.Payload, &p); err == nil && p.DurationMS > 0 {
			d = time.Duration(p.DurationMS) * time.Millisecond
		}
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// flakyPayload is the payload flakyHandler understands: {"fail_times": 2}.
type flakyPayload struct {
	FailTimes int `json:"fail_times"`
}

// flakyHandler fails on its first FailTimes attempts and succeeds after that.
// It decides off t.Attempts, incremented by the claim query itself, rather
// than local state, so it stays correct under at-least-once redelivery.
func flakyHandler(_ context.Context, t domain.Task) error {
	failTimes := 2
	var p flakyPayload
	if len(t.Payload) > 0 {
		if err := json.Unmarshal(t.Payload, &p); err == nil {
			failTimes = p.FailTimes
		}
	}
	if t.Attempts <= failTimes {
		return errors.New("flaky: simulated failure")
	}
	return nil
}
