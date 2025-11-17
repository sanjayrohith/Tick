package materializer

import (
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestPlanFromLastMaterializedAt(t *testing.T) {
	last := time.Date(2025, 11, 17, 0, 0, 0, 0, time.UTC)
	s := &domain.Schedule{
		ID:                 1,
		Cron:               "0 0 * * *",
		Timezone:           "UTC",
		LastMaterializedAt: &last,
	}
	now := time.Date(2025, 11, 17, 12, 0, 0, 0, time.UTC)

	got, err := Plan(s, now, 72*time.Hour)
	if err != nil {
		t.Fatalf("Plan() = %v, want success", err)
	}

	want := []time.Time{
		time.Date(2025, 11, 18, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 11, 19, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 11, 20, 0, 0, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if !got[i].Equal(w) {
			t.Errorf("got[%d] = %v, want %v", i, got[i], w)
		}
	}
}

func TestPlanWithNoPriorMaterializationStartsFromNow(t *testing.T) {
	s := &domain.Schedule{ID: 1, Cron: "0 0 * * *", Timezone: "UTC"}
	now := time.Date(2025, 11, 17, 12, 0, 0, 0, time.UTC)

	got, err := Plan(s, now, 24*time.Hour)
	if err != nil {
		t.Fatalf("Plan() = %v, want success", err)
	}
	// A schedule that never materialized before owes nothing prior to now;
	// the next midnight is the only fire within a 24h horizon.
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1: %v", len(got), got)
	}
	if want := time.Date(2025, 11, 18, 0, 0, 0, 0, time.UTC); !got[0].Equal(want) {
		t.Errorf("got[0] = %v, want %v", got[0], want)
	}
}

func TestPlanClampsToHorizon(t *testing.T) {
	s := &domain.Schedule{ID: 1, Cron: "0 0 * * *", Timezone: "UTC"}
	now := time.Date(2025, 11, 17, 0, 0, 0, 0, time.UTC)

	got, err := Plan(s, now, time.Hour)
	if err != nil {
		t.Fatalf("Plan() = %v, want success", err)
	}
	if len(got) != 0 {
		t.Errorf("len(got) = %d, want 0 within a 1h horizon", len(got))
	}
}

func TestPlanClampsPathologicalScheduleToMaxCount(t *testing.T) {
	s := &domain.Schedule{ID: 1, Cron: "@every 1s"}
	now := time.Date(2025, 11, 17, 0, 0, 0, 0, time.UTC)

	got, err := Plan(s, now, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("Plan() = %v, want success", err)
	}
	if len(got) != MaxPlannedFireTimes {
		t.Errorf("len(got) = %d, want the clamp of %d", len(got), MaxPlannedFireTimes)
	}
}

func TestPlanRejectsInvalidCron(t *testing.T) {
	s := &domain.Schedule{ID: 1, Cron: "not a cron expr"}
	_, err := Plan(s, time.Now(), time.Hour)
	if !errors.Is(err, domain.ErrInvalidCron) {
		t.Fatalf("error = %v, want it to wrap ErrInvalidCron", err)
	}
}

func TestPlanRejectsInvalidTimezone(t *testing.T) {
	s := &domain.Schedule{ID: 1, Cron: "0 0 * * *", Timezone: "Mars/Olympus_Mons"}
	_, err := Plan(s, time.Now(), time.Hour)
	if !errors.Is(err, domain.ErrInvalidTimezone) {
		t.Fatalf("error = %v, want it to wrap ErrInvalidTimezone", err)
	}
}
