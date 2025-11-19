package materializer

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

// testConnStr and testPool back the multi-node safety test below: proving
// that concurrent materializers rely on the real database's unique index,
// not an in-memory fake, is the entire point of that test.
var (
	testConnStr string
	testPool    *pgxpool.Pool
)

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

	testConnStr = connStr
	testPool = pool
	return m.Run()
}

// truncateAll clears every table between tests.
func truncateAll(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := testPool.Exec(ctx, "TRUNCATE tasks, schedules RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}
