package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

// scheduleColumns lists every column of the schedules table in the order
// scanSchedule expects them.
const scheduleColumns = `id, cron, timezone, queue, handler, payload, priority,
	max_attempts, enabled, last_materialized_at, created_at`

func scanSchedule(row pgx.Row) (*domain.Schedule, error) {
	var s domain.Schedule
	err := row.Scan(
		&s.ID, &s.Cron, &s.Timezone, &s.Queue, &s.Handler, &s.Payload, &s.Priority,
		&s.MaxAttempts, &s.Enabled, &s.LastMaterializedAt, &s.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

const createScheduleSQL = `
INSERT INTO schedules (cron, timezone, queue, handler, payload, priority, max_attempts)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING ` + scheduleColumns

// CreateSchedule inserts one schedule and returns it with its assigned ID and
// any database-applied defaults.
func (s *Store) CreateSchedule(ctx context.Context, sc *domain.Schedule) (*domain.Schedule, error) {
	timezone := sc.Timezone
	if timezone == "" {
		timezone = domain.DefaultTimezone
	}
	queue := sc.Queue
	if queue == "" {
		queue = "default"
	}
	maxAttempts := sc.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}

	row := s.pool.QueryRow(ctx, createScheduleSQL,
		sc.Cron, timezone, queue, sc.Handler, sc.Payload, sc.Priority, maxAttempts)
	created, err := scanSchedule(row)
	if err != nil {
		return nil, fmt.Errorf("creating schedule: %w", err)
	}
	return created, nil
}

const getScheduleSQL = `SELECT ` + scheduleColumns + ` FROM schedules WHERE id = $1`

// GetSchedule returns one schedule by ID, or domain.ErrNotFound.
func (s *Store) GetSchedule(ctx context.Context, id int64) (*domain.Schedule, error) {
	sc, err := scanSchedule(s.pool.QueryRow(ctx, getScheduleSQL, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("getting schedule %d: %w", id, err)
	}
	return sc, nil
}

const listSchedulesSQL = `SELECT ` + scheduleColumns + ` FROM schedules ORDER BY id`

// ListSchedules returns every schedule, enabled or not, ordered by ID.
func (s *Store) ListSchedules(ctx context.Context) ([]*domain.Schedule, error) {
	rows, err := s.pool.Query(ctx, listSchedulesSQL)
	if err != nil {
		return nil, fmt.Errorf("listing schedules: %w", err)
	}
	defer rows.Close()
	return collectSchedules(rows)
}

const listEnabledSchedulesSQL = `SELECT ` + scheduleColumns + ` FROM schedules WHERE enabled ORDER BY id`

// ListEnabledSchedules returns every schedule not administratively disabled,
// backing the materializer's scan of what it has to do on each pass.
func (s *Store) ListEnabledSchedules(ctx context.Context) ([]*domain.Schedule, error) {
	rows, err := s.pool.Query(ctx, listEnabledSchedulesSQL)
	if err != nil {
		return nil, fmt.Errorf("listing enabled schedules: %w", err)
	}
	defer rows.Close()
	return collectSchedules(rows)
}

func collectSchedules(rows pgx.Rows) ([]*domain.Schedule, error) {
	var out []*domain.Schedule
	for rows.Next() {
		sc, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning schedule: %w", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading schedules: %w", err)
	}
	return out, nil
}

const setScheduleEnabledSQL = `
UPDATE schedules SET enabled = $2 WHERE id = $1
RETURNING ` + scheduleColumns

// SetScheduleEnabled enables or disables a schedule and returns it as
// updated. Disabling stops future materialization; it deliberately leaves
// every task already materialized untouched.
func (s *Store) SetScheduleEnabled(ctx context.Context, id int64, enabled bool) (*domain.Schedule, error) {
	sc, err := scanSchedule(s.pool.QueryRow(ctx, setScheduleEnabledSQL, id, enabled))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("setting schedule %d enabled=%t: %w", id, enabled, err)
	}
	return sc, nil
}

// advanceLastMaterializedAtSQL only ever moves last_materialized_at forward.
// The guard matters under concurrent materializers: a node that is behind
// and still processing an earlier, smaller batch must not be able to undo
// the progress a faster node already recorded for the same schedule.
const advanceLastMaterializedAtSQL = `
UPDATE schedules
SET last_materialized_at = $2
WHERE id = $1 AND (last_materialized_at IS NULL OR last_materialized_at < $2)`

// AdvanceLastMaterializedAt moves a schedule's resume point forward to at.
func (s *Store) AdvanceLastMaterializedAt(ctx context.Context, scheduleID int64, at time.Time) error {
	if _, err := s.pool.Exec(ctx, advanceLastMaterializedAtSQL, scheduleID, at); err != nil {
		return fmt.Errorf("advancing last_materialized_at for schedule %d: %w", scheduleID, err)
	}
	return nil
}
