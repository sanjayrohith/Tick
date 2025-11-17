package cron

import (
	"testing"
	"time"
)

func TestNextNFiresOnceOnAmbiguousFallBackTime(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}

	// 01:30 on 2026-11-01 occurs twice: once at -04:00 (EDT, before the
	// transition) and once at -05:00 (EST, after it). The schedule must fire
	// only on the first occurrence.
	after := time.Date(2026, 10, 30, 0, 0, 0, 0, loc)
	got, err := NextN("30 1 * * *", "America/New_York", after, 3)
	if err != nil {
		t.Fatalf("NextN() = %v, want success", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}

	want := []time.Time{
		time.Date(2026, 10, 30, 1, 30, 0, 0, loc),
		time.Date(2026, 10, 31, 1, 30, 0, 0, loc),
		time.Date(2026, 11, 1, 1, 30, 0, 0, loc), // the pre-transition (EDT) occurrence
	}
	for i, w := range want {
		if !got[i].Equal(w) {
			t.Errorf("got[%d] = %v, want %v", i, got[i].In(loc), w)
		}
	}

	// The fire on 2026-11-01 must carry the EDT offset, not EST: that is
	// what makes it the first occurrence rather than the second.
	_, offset := got[2].In(loc).Zone()
	if wantOffset := -4 * 60 * 60; offset != wantOffset {
		t.Errorf("2026-11-01 fire offset = %ds, want %ds (EDT, pre-transition)", offset, wantOffset)
	}

	// Continuing past the ambiguous day proves the second (EST) occurrence
	// was skipped rather than merely delayed.
	more, err := NextN("30 1 * * *", "America/New_York", got[2], 1)
	if err != nil {
		t.Fatalf("NextN() = %v, want success", err)
	}
	if want := time.Date(2026, 11, 2, 1, 30, 0, 0, loc); !more[0].Equal(want) {
		t.Errorf("next fire after the ambiguous day = %v, want %v", more[0].In(loc), want)
	}
}
