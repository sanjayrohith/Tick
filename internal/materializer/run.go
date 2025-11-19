package materializer

import (
	"context"
	"log/slog"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
)

// DefaultInterval is how often the run loop checks schedules for new work
// when no other interval is configured.
const DefaultInterval = 30 * time.Second

// DefaultHorizon is how far ahead of now the run loop materializes work when
// no other horizon is configured.
const DefaultHorizon = 5 * time.Minute

// ScheduleLister is the store surface the run loop needs to find work.
type ScheduleLister interface {
	// ListEnabledSchedules returns every schedule not administratively
	// disabled. A disabled schedule is skipped entirely: it owes nothing
	// while disabled, and resumes exactly where it left off once re-enabled.
	ListEnabledSchedules(ctx context.Context) ([]*domain.Schedule, error)
}

// ScheduleAdvancer is the store surface the run loop needs to record
// progress.
type ScheduleAdvancer interface {
	// AdvanceLastMaterializedAt moves a schedule's resume point forward to
	// at. Implementations must never move it backward, so that a concurrent
	// advance from another node racing on a later fire time cannot be
	// undone by a slower one still processing an earlier batch.
	AdvanceLastMaterializedAt(ctx context.Context, scheduleID int64, at time.Time) error
}

// Store is the store surface Runner needs: listing schedules, inserting the
// tasks they generate, and recording how far each one has been materialized.
type Store interface {
	Enqueuer
	ScheduleLister
	ScheduleAdvancer
}

// Runner periodically materializes every enabled schedule.
type Runner struct {
	store    Store
	horizon  time.Duration
	interval time.Duration
	log      *slog.Logger
}

// New builds a Runner. A zero horizon or interval falls back to its default,
// and a nil logger discards output.
func New(store Store, horizon, interval time.Duration, log *slog.Logger) *Runner {
	if horizon <= 0 {
		horizon = DefaultHorizon
	}
	if interval <= 0 {
		interval = DefaultInterval
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Runner{store: store, horizon: horizon, interval: interval, log: log}
}

// Run materializes on a ticker until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	r.RunOnce(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce materializes every enabled schedule once. A schedule that fails to
// plan or insert -- most often a cron expression that no longer parses -- is
// logged and skipped, with the loop continuing to every other schedule
// rather than aborting the pass. The failing schedule is left enabled: a
// materializer disabling schedules on its own initiative would be a much
// louder failure mode than simply not making progress on one of them.
func (r *Runner) RunOnce(ctx context.Context) {
	schedules, err := r.store.ListEnabledSchedules(ctx)
	if err != nil {
		r.log.ErrorContext(ctx, "listing enabled schedules failed", "error", err)
		return
	}

	now := time.Now()
	for _, s := range schedules {
		if err := r.materializeOne(ctx, now, s); err != nil {
			r.log.ErrorContext(ctx, "materializing schedule failed", "schedule_id", s.ID, "error", err)
			continue
		}
	}
}

// materializeOne plans and inserts one schedule's due work, advancing its
// resume point only once the insert has actually succeeded -- a schedule
// whose insert fails must be retried from the same point next pass, not
// skipped as if it had already run.
func (r *Runner) materializeOne(ctx context.Context, now time.Time, s *domain.Schedule) error {
	fireTimes, err := Plan(s, now, r.horizon)
	if err != nil {
		return err
	}
	if len(fireTimes) == 0 {
		return nil
	}

	if _, err := Materialize(ctx, r.store, s, fireTimes); err != nil {
		return err
	}

	return r.store.AdvanceLastMaterializedAt(ctx, s.ID, fireTimes[len(fireTimes)-1])
}
