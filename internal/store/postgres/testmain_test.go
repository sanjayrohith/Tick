package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sanjayrohith/tick/internal/migrate"
)

// testPool is shared by every test in this package. One container is started
// for the whole package run; tests isolate themselves with truncateAll rather
// than each paying for a fresh container.
var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tick_test"),
		tcpostgres.WithUsername("tick"),
		tcpostgres.WithPassword("tick"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting postgres container: %v\n", err)
		return 1
	}
	defer func() {
		if termErr := container.Terminate(ctx); termErr != nil {
			fmt.Fprintf(os.Stderr, "terminating postgres container: %v\n", termErr)
		}
	}()

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading connection string: %v\n", err)
		return 1
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connecting to test database: %v\n", err)
		return 1
	}
	defer pool.Close()

	if _, err := migrate.Apply(ctx, pool, slog.New(slog.DiscardHandler)); err != nil {
		fmt.Fprintf(os.Stderr, "applying migrations: %v\n", err)
		return 1
	}

	testPool = pool
	return m.Run()
}

// truncateAll clears every table between tests, so each test starts from an
// empty database without the cost of a fresh container.
func truncateAll(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := testPool.Exec(ctx, "TRUNCATE tasks, schedules RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}

// TestHarnessAppliesMigrationsAndTruncates proves the shared container is
// migrated and that truncateAll actually clears state a test left behind.
func TestHarnessAppliesMigrationsAndTruncates(t *testing.T) {
	truncateAll(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := testPool.Exec(ctx,
		"INSERT INTO tasks (handler, payload, run_at) VALUES ('noop', '{}', now())"); err != nil {
		t.Fatalf("inserting a probe row: %v", err)
	}

	truncateAll(t)

	var count int
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("counting tasks: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d after truncateAll, want 0", count)
	}
}
