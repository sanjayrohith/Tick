package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestCreateScheduleAppliesDefaults(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	created, err := s.CreateSchedule(ctx, &domain.Schedule{
		Cron:    "30 2 * * *",
		Handler: "send_digest",
		Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateSchedule() = %v, want success", err)
	}
	if created.ID == 0 {
		t.Error("ID = 0, want a database-assigned id")
	}
	if created.Timezone != domain.DefaultTimezone {
		t.Errorf("Timezone = %q, want %q", created.Timezone, domain.DefaultTimezone)
	}
	if created.Queue != "default" {
		t.Errorf("Queue = %q, want %q", created.Queue, "default")
	}
	if created.MaxAttempts != 5 {
		t.Errorf("MaxAttempts = %d, want 5", created.MaxAttempts)
	}
	if !created.Enabled {
		t.Error("Enabled = false, want a new schedule to start enabled")
	}
}

func TestGetScheduleNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.GetSchedule(context.Background(), 999999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestListSchedulesAndListEnabledSchedules(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	enabled, err := s.CreateSchedule(ctx, &domain.Schedule{
		Cron: "0 0 * * *", Handler: "noop", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateSchedule() = %v", err)
	}
	disabled, err := s.CreateSchedule(ctx, &domain.Schedule{
		Cron: "0 0 * * *", Handler: "noop", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateSchedule() = %v", err)
	}
	if _, err = s.SetScheduleEnabled(ctx, disabled.ID, false); err != nil {
		t.Fatalf("SetScheduleEnabled() = %v", err)
	}

	all, err := s.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules() = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}

	onlyEnabled, err := s.ListEnabledSchedules(ctx)
	if err != nil {
		t.Fatalf("ListEnabledSchedules() = %v", err)
	}
	if len(onlyEnabled) != 1 || onlyEnabled[0].ID != enabled.ID {
		t.Errorf("ListEnabledSchedules() = %+v, want only schedule %d", onlyEnabled, enabled.ID)
	}
}

func TestSetScheduleEnabledNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.SetScheduleEnabled(context.Background(), 999999, false)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestAdvanceLastMaterializedAtOnlyMovesForward(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	sc, err := s.CreateSchedule(ctx, &domain.Schedule{
		Cron: "0 0 * * *", Handler: "noop", Payload: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateSchedule() = %v", err)
	}

	later := time.Date(2025, 11, 19, 12, 0, 0, 0, time.UTC)
	if err = s.AdvanceLastMaterializedAt(ctx, sc.ID, later); err != nil {
		t.Fatalf("AdvanceLastMaterializedAt() = %v", err)
	}

	earlier := later.Add(-time.Hour)
	if err = s.AdvanceLastMaterializedAt(ctx, sc.ID, earlier); err != nil {
		t.Fatalf("AdvanceLastMaterializedAt() = %v", err)
	}

	got, err := s.GetSchedule(ctx, sc.ID)
	if err != nil {
		t.Fatalf("GetSchedule() = %v", err)
	}
	if got.LastMaterializedAt == nil || !got.LastMaterializedAt.Equal(later) {
		t.Errorf("LastMaterializedAt = %v, want it to stay at %v after an earlier advance", got.LastMaterializedAt, later)
	}
}
