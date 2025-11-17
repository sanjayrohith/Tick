package cron

import (
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestNextNComputesUTCInstantsFromLocalWallClock(t *testing.T) {
	after := time.Date(2025, 11, 6, 0, 0, 0, 0, time.UTC)

	got, err := NextN("30 2 * * *", "Asia/Kolkata", after, 3)
	if err != nil {
		t.Fatalf("NextN() = %v, want success", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}

	// 02:30 IST is 21:00 UTC the previous day (IST is UTC+5:30).
	want := []time.Time{
		time.Date(2025, 11, 6, 21, 0, 0, 0, time.UTC),
		time.Date(2025, 11, 7, 21, 0, 0, 0, time.UTC),
		time.Date(2025, 11, 8, 21, 0, 0, 0, time.UTC),
	}
	for i, w := range want {
		if !got[i].Equal(w) {
			t.Errorf("got[%d] = %v, want %v", i, got[i], w)
		}
		if got[i].Location() != time.UTC {
			t.Errorf("got[%d].Location() = %v, want UTC", i, got[i].Location())
		}
	}
}

func TestNextNStrictlyAfterGivenInstant(t *testing.T) {
	// Landing exactly on a fire time must not return that same instant again.
	after := time.Date(2025, 11, 6, 21, 0, 0, 0, time.UTC)

	got, err := NextN("30 2 * * *", "Asia/Kolkata", after, 1)
	if err != nil {
		t.Fatalf("NextN() = %v, want success", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Equal(after) {
		t.Error("NextN returned the given instant instead of strictly after it")
	}
	if want := after.Add(24 * time.Hour); !got[0].Equal(want) {
		t.Errorf("got[0] = %v, want %v", got[0], want)
	}
}

func TestNextNRejectsUnloadableTimezone(t *testing.T) {
	_, err := NextN("30 2 * * *", "Mars/Olympus_Mons", time.Now(), 1)
	if !errors.Is(err, domain.ErrInvalidTimezone) {
		t.Fatalf("error = %v, want it to wrap ErrInvalidTimezone", err)
	}
}

func TestNextNRejectsInvalidExpression(t *testing.T) {
	_, err := NextN("not a cron expr", "UTC", time.Now(), 1)
	if !errors.Is(err, domain.ErrInvalidCron) {
		t.Fatalf("error = %v, want it to wrap ErrInvalidCron", err)
	}
}

func TestNextNZeroOrNegativeCount(t *testing.T) {
	sched, err := Parse("30 2 * * *")
	if err != nil {
		t.Fatalf("Parse() = %v", err)
	}
	if got := sched.NextN(time.UTC, time.Now(), 0); got != nil {
		t.Errorf("NextN(n=0) = %v, want nil", got)
	}
	if got := sched.NextN(time.UTC, time.Now(), -1); got != nil {
		t.Errorf("NextN(n=-1) = %v, want nil", got)
	}
}
