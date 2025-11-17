package cron

import (
	"testing"
	"time"
)

func TestNextNSkipsNonexistentSpringForwardTime(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 02:30 on 2026-03-08 never occurs: the clock jumps from 02:00 EST
	// straight to 03:00 EDT. The fire due that day must not appear at all,
	// not be shifted to the nearest valid instant.
	after := time.Date(2026, 3, 6, 0, 0, 0, 0, loc)
	got, err := NextN("30 2 * * *", "America/New_York", after, 3)
	if err != nil {
		t.Fatalf("NextN() = %v, want success", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}

	for _, fire := range got {
		local := fire.In(loc)
		if local.Month() == time.March && local.Day() == 8 {
			t.Errorf("got a fire time on the spring-forward day: %v", local)
		}
	}

	// The day after the gap should still fire normally, at 02:30 EDT.
	want := time.Date(2026, 3, 9, 2, 30, 0, 0, loc)
	if last := got[len(got)-1]; !last.Equal(want) {
		t.Errorf("last fire = %v, want %v", last.In(loc), want)
	}
}

func TestRoundTripsDetectsMismatchedWallClock(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// A UTC instant's wall-clock fields, reinterpreted as if they were
	// already in loc, describe a different moment entirely: reconstructing
	// them in loc must not reproduce the original instant.
	utcInstant := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	if roundTrips(utcInstant, loc) {
		t.Error("roundTrips() = true for a wall clock that does not describe the same instant in loc")
	}

	// A genuine instant already expressed in loc must round-trip cleanly.
	localInstant := time.Date(2025, 6, 1, 12, 0, 0, 0, loc)
	if !roundTrips(localInstant, loc) {
		t.Error("roundTrips() = false for an instant already expressed in loc")
	}
}
