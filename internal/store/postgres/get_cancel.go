package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

const getByIDSQL = `SELECT ` + taskColumns + ` FROM tasks WHERE id = $1`

// Get returns one task by ID, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id int64) (*domain.Task, error) {
	t, err := scanTask(s.pool.QueryRow(ctx, getByIDSQL, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("getting task %d: %w", id, err)
	}
	return t, nil
}

// cancelSQL only ever touches a pending row: once a task is running its side
// effects are already in flight, and once it is terminal there is nothing
// left to cancel.
const cancelSQL = `
UPDATE tasks
SET status = 'cancelled'
WHERE id = $1 AND status = 'pending'`

// Cancel transitions a pending task to cancelled. It returns
// ErrNotCancellable when the task is already running or terminal, and
// ErrNotFound when no task with this id exists at all.
func (s *Store) Cancel(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, cancelSQL, id)
	if err != nil {
		return fmt.Errorf("cancelling task %d: %w", id, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	// The update matched nothing: find out whether that is because the task
	// does not exist, or because it exists but is not pending, so the caller
	// gets the right error for each.
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return domain.ErrNotCancellable
}
