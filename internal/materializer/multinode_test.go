package materializer

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/sanjayrohith/tick/internal/domain"
	"github.com/sanjayrohith/tick/internal/store/postgres"
)

// TestConcurrentMaterializersProduceNoDuplicateTasks is the multi-node
// safety proof: any number of scheduler nodes may materialize the same
// schedule at the same time, and the database's unique index on (queue,
// idempotency_key) must turn every insert after the first into a no-op. If
// this test ever produces more rows than fire times, the whole "any number
// of nodes" claim in domain.Schedule's package comment is false.
func TestConcurrentMaterializersProduceNoDuplicateTasks(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	// A separate postgres.Store per "node", each with its own connection
	// pool, so the test exercises real concurrent connections racing the
	// same rows rather than one pool serialising everything itself.
	const nodes = 8
	stores := make([]*postgres.Store, nodes)
	for i := range stores {
		s, err := postgres.New(ctx, testConnStr, postgres.DefaultConfig())
		if err != nil {
			t.Fatalf("postgres.New() = %v", err)
		}
		defer func() { _ = s.Close() }()
		stores[i] = s
	}

	// Several schedules, several horizons: a mix of cron granularities so
	// the total expected fire count is not the same trivial number for
	// every schedule.
	specs := []struct {
		cron    string
		horizon time.Duration
	}{
		{"* * * * *", time.Hour},       // 60 fires
		{"*/5 * * * *", 2 * time.Hour}, // 24 fires
		{"0 * * * *", 24 * time.Hour},  // 24 fires
	}

	now := time.Date(2025, 11, 19, 0, 0, 0, 0, time.UTC)

	for _, spec := range specs {
		sc, err := stores[0].CreateSchedule(ctx, &domain.Schedule{
			Cron:    spec.cron,
			Handler: "noop",
			Payload: json.RawMessage(`{}`),
			Queue:   "default",
		})
		if err != nil {
			t.Fatalf("CreateSchedule(%q) = %v", spec.cron, err)
		}

		wantFires, err := Plan(sc, now, spec.horizon)
		if err != nil {
			t.Fatalf("Plan(%q) = %v", spec.cron, err)
		}
		if len(wantFires) == 0 {
			t.Fatalf("Plan(%q) produced no fire times, test is not exercising anything", spec.cron)
		}

		// Every node races to materialize the identical fire time set for
		// this schedule at once.
		var wg sync.WaitGroup
		for _, s := range stores {
			wg.Add(1)
			go func(s *postgres.Store) {
				defer wg.Done()
				if _, err := Materialize(ctx, s, sc, wantFires); err != nil {
					t.Errorf("Materialize(%q) = %v, want success", spec.cron, err)
				}
			}(s)
		}
		wg.Wait()

		var count int
		if err := testPool.QueryRow(ctx,
			"SELECT count(*) FROM tasks WHERE schedule_id = $1", sc.ID).Scan(&count); err != nil {
			t.Fatalf("counting materialized tasks: %v", err)
		}
		if count != len(wantFires) {
			t.Errorf("schedule %q: task count = %d after %d concurrent materializers, want exactly %d (no duplicates)",
				spec.cron, count, nodes, len(wantFires))
		}
	}
}
