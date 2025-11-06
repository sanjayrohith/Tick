// Package migrate applies embedded SQL migrations to Postgres.
//
// Migrations are forward-only. There are no down migrations, because a rollback
// that drops a column is not a rollback, it is data loss with extra steps. To
// undo a migration, write the next one.
//
// Every Tick binary calls Apply on startup. That is safe under concurrent boots:
// the whole run is serialized behind a Postgres advisory lock, so a rolling
// deploy of thirty workers applies each migration exactly once.
package migrate

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed sql/*.sql
var migrationFS embed.FS

// lockID is an arbitrary but fixed key for pg_advisory_lock. Any Tick process
// migrating the same database uses this same value, which is what serializes
// them. The number has no meaning beyond being unlikely to collide with another
// application's advisory locks.
const lockID int64 = 8_675_309_042

// filenamePattern matches "0001_description.sql". The numeric prefix is the
// version and must be unique and gapless-ordered; the description is for humans.
var filenamePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// Migration is one versioned SQL file.
type Migration struct {
	// Version is the numeric prefix, for example 1 for "0001_tasks.sql".
	Version int
	// Name is the descriptive part, for example "tasks".
	Name string
	// SQL is the file's contents.
	SQL string
}

func (m Migration) String() string {
	return fmt.Sprintf("%04d_%s", m.Version, m.Name)
}

// Load reads and parses the embedded migrations, sorted by version.
func Load() ([]Migration, error) {
	return loadFrom(migrationFS, "sql")
}

func loadFrom(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading migration directory: %w", err)
	}

	migrations := make([]Migration, 0, len(entries))
	seen := make(map[int]string, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := filenamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf(
				"migration %q does not match NNNN_description.sql", e.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("migration %q has an unparseable version: %w", e.Name(), err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf(
				"migrations %q and %q share version %d", prev, e.Name(), version)
		}
		seen[version] = e.Name()

		body, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", e.Name(), err)
		}
		if strings.TrimSpace(string(body)) == "" {
			return nil, fmt.Errorf("migration %q is empty", e.Name())
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    m[2],
			SQL:     string(body),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return migrations, nil
}

// schemaTableDDL creates the bookkeeping table. It runs outside the migration
// list because it must exist before any version can be recorded.
const schemaTableDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INT PRIMARY KEY,
  name        TEXT NOT NULL,
  applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Applier is the minimal pgx surface Apply needs: the ability to start a
// transaction. Both *pgx.Conn and *pgxpool.Pool satisfy it, and so does a test
// double. Everything else happens on the transaction.
type Applier interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Apply brings the database up to the latest embedded version.
//
// The sequence is: take the advisory lock, create the bookkeeping table, read
// which versions are already applied, then run each missing migration inside its
// own transaction that also records the version. Migration and bookkeeping
// committing together is what makes a crash mid-run safe: a migration is either
// fully applied and recorded, or neither.
func Apply(ctx context.Context, pool Applier, log *slog.Logger) (applied int, err error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	migrations, loadErr := Load()
	if loadErr != nil {
		return 0, loadErr
	}
	if len(migrations) == 0 {
		return 0, errors.New("no migrations are embedded in the binary")
	}

	// Serialize every migrating process behind one lock. Held on a dedicated
	// transaction for the whole run and released by its commit.
	lockTx, beginErr := pool.Begin(ctx)
	if beginErr != nil {
		return 0, fmt.Errorf("beginning lock transaction: %w", beginErr)
	}
	defer func() {
		// Rollback releases the advisory lock just as well as commit; this
		// transaction never writes anything.
		if rbErr := lockTx.Rollback(ctx); rbErr != nil &&
			!errors.Is(rbErr, pgx.ErrTxClosed) && err == nil {
			err = fmt.Errorf("releasing migration lock: %w", rbErr)
		}
	}()

	start := time.Now()
	if _, lockErr := lockTx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID); lockErr != nil {
		return 0, fmt.Errorf("acquiring migration lock: %w", lockErr)
	}
	if waited := time.Since(start); waited > time.Second {
		log.InfoContext(ctx, "waited for the migration lock",
			"waited_seconds", waited.Seconds())
	}

	if _, ddlErr := lockTx.Exec(ctx, schemaTableDDL); ddlErr != nil {
		return 0, fmt.Errorf("creating schema_migrations: %w", ddlErr)
	}

	done, listErr := appliedVersions(ctx, lockTx)
	if listErr != nil {
		return 0, listErr
	}

	for _, m := range migrations {
		if done[m.Version] {
			continue
		}
		if applyErr := applyOne(ctx, lockTx, m); applyErr != nil {
			return applied, applyErr
		}
		applied++
		log.InfoContext(ctx, "applied migration",
			"version", m.Version, "name", m.Name)
	}

	if commitErr := lockTx.Commit(ctx); commitErr != nil {
		return applied, fmt.Errorf("committing migrations: %w", commitErr)
	}
	return applied, nil
}

func appliedVersions(ctx context.Context, tx pgx.Tx) (map[int]bool, error) {
	rows, err := tx.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("reading applied versions: %w", err)
	}
	defer rows.Close()

	done := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning applied version: %w", err)
		}
		done[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating applied versions: %w", err)
	}
	return done, nil
}

// applyOne runs a migration's SQL and records it. Both happen on the same
// transaction that holds the advisory lock, so the whole run is atomic.
func applyOne(ctx context.Context, tx pgx.Tx, m Migration) error {
	if _, err := tx.Exec(ctx, m.SQL); err != nil {
		return fmt.Errorf("applying migration %s: %w", m, err)
	}
	_, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		m.Version, m.Name)
	if err != nil {
		return fmt.Errorf("recording migration %s: %w", m, err)
	}
	return nil
}
