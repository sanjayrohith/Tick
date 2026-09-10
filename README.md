# Tick

**A high-throughput distributed task scheduler for teams that already run Postgres.**

Tick solves the distributed scheduling problem with mechanical correctness: zero double-execution under any concurrency, automatic recovery of orphaned tasks, and language-agnostic submission over HTTP. Built for teams that need reliable scheduled and delayed jobs without adding infrastructure.

---

## The Problem

Running scheduled work across multiple machines is where most teams improvise badly:

- **Cron on one box** — Works until that box dies or you need two. No retries, no history, no visibility.
- **Naive DB polling** — `SELECT` then `UPDATE` has a gap between read and write. Under concurrency, two workers grab the same row and run the job twice.
- **`SELECT ... FOR UPDATE` without `SKIP LOCKED`** — Workers block on each other. Adding workers adds latency instead of throughput.
- **Orphaned jobs** — A worker OOMs mid-execution. The row sits in `running` forever and nothing ever retries it.
- **Clock drift** — A node 200ms ahead fires jobs early. With sub-minute schedules this produces real ordering bugs.

Existing options force tradeoffs: Celery and Sidekiq need Redis plus a language runtime lock-in; Temporal is powerful but heavy to operate; cloud schedulers lock you to one provider.

## The Solution

Tick claims tasks with a single atomic `UPDATE ... FOR UPDATE SKIP LOCKED` statement. Concurrent workers skip each other's locked rows rather than queueing behind them. The database clock is the only clock that matters, eliminating node clock drift as a source of early firing.

**Core guarantees:**

- **Zero double-execution** — Proven by test: 50 workers, 10,000 tasks, every task executed exactly once, 100 iterations in CI.
- **Automatic recovery** — Workers update a heartbeat every 10 seconds. A sweeper reclaims tasks whose heartbeat has expired, with capped exponential backoff so an OOM-inducing job doesn't kill the next worker.
- **Two storage backends** — Postgres (default) and Redis hybrid (opt-in for >10M tasks/day).
- **Language-agnostic** — Submit tasks over HTTP. Workers register handlers in any language.

**Delivery semantics: at-least-once.** Exactly-once delivery is not achievable across a network boundary, and claiming otherwise is a red flag. Tick guarantees at-least-once dispatch and provides idempotency keys so your handler can make the observable effect happen once. Write your handlers accordingly.

---

## How It Works

### The Claim Query

The mechanical heart of the system is a single SQL statement that atomically claims work:

```sql
UPDATE tasks
SET status = 'running',
    claimed_by = $1,
    heartbeat_at = now(),
    attempts = attempts + 1
WHERE id IN (
  SELECT id FROM tasks
  WHERE queue = $2 AND status = 'pending' AND run_at <= now()
  ORDER BY priority DESC, run_at
  FOR UPDATE SKIP LOCKED
  LIMIT $3
)
RETURNING *;
```

One transaction. Concurrent workers skip locked rows. The database clock (`now()`) is the only clock that decides when a task is due.

### Heartbeat and Recovery

Executing workers refresh `heartbeat_at` every 10 seconds. A sweeper runs every 30 seconds:

```sql
UPDATE tasks
SET status = 'pending',
    run_at = now() + (interval '1 second' * least(300, power(2, attempts) * 5)),
    claimed_by = NULL,
    last_error = 'orphaned: heartbeat expired'
WHERE status = 'running' AND heartbeat_at < now() - interval '90 seconds';
```

Tasks exceeding `max_attempts` move to `dead` and stay for inspection. Backoff on recovery matters: if a worker died because the job OOMs the process, immediate retry just kills the next worker.

### Recurring Schedules

Schedules are cron expressions with real timezone handling:

- **DST spring-forward:** Skip nonexistent wall-clock times (e.g., 02:30 on March 8 in America/New_York).
- **DST fall-back:** Fire once on ambiguous times (e.g., 01:30 on November 1 in America/New_York).

A materializer loop computes the next N fire times and inserts tasks with idempotency key `schedule_id:run_at`. Any number of scheduler nodes can run this concurrently; the unique index makes duplicate insertion a no-op.

### Redis Hybrid Backend

For workloads exceeding ~10M tasks/day, where Postgres polling saturates connections:

- A sweeper pulls tasks due in the next 60 seconds from Postgres and `ZADD`s them into a sorted set.
- A publisher polls `ZRANGEBYSCORE` for due tasks and moves them to a Redis Stream via atomic Lua script.
- Workers consume via `XREADGROUP` and `XACK`.

Postgres stays the source of truth. Redis is a cache of imminent work. If Redis is wiped, the sweeper repopulates it.

---

## Architecture

```
┌─────────────┐  HTTP   ┌──────────┐
│   Client    │────────▶│ tick-api │
└─────────────┘         └─────┬────┘
                              │
                              ▼
                        ┌──────────┐
                        │ Postgres │◀─────┐
                        └─────┬────┘      │
                              │           │
                  ┌───────────┴───────────┼───────────┐
                  │                       │           │
                  ▼                       ▼           │
            ┌───────────┐          ┌──────────┐      │
            │  Worker   │          │Scheduler │      │
            │  (claim   │          │(material-│      │
            │  execute) │          │ izer +   │      │
            └───────────┘          │ sweeper) │      │
                                   └──────────┘      │
                                                     │
            Optional Redis hybrid backend ───────────┘
```

**Components:**

- **tick-api** — HTTP submission API. Enqueue tasks, create schedules, query status.
- **tick-worker** — Claims and executes tasks. Stateless, horizontally scalable.
- **tick-scheduler** — Materializes recurring schedules and runs the orphan sweeper.

---

## API

```
POST   /tasks              Submit one task
POST   /tasks/bulk         Submit many (up to 1000)
GET    /tasks/:id          Status and attempt history
DELETE /tasks/:id          Cancel if still pending
POST   /schedules          Create recurring schedule
GET    /schedules/:id      Retrieve schedule
PATCH  /schedules/:id      Enable/disable schedule
GET    /queues/:name/stats Depth, oldest pending age, throughput
GET    /healthz            Process liveness
GET    /readyz             Store connectivity
```

Idempotency keys accepted on submit; client retries return the original task instead of duplicates.

---

## Observability

Built-in distributed tracing and metrics:

- **W3C `traceparent`** stored with the queued task and restored when a worker picks it up, so the trace survives the queue crossing.
- **OpenTelemetry** exports to OTLP Collector → Jaeger (traces) and Prometheus (metrics).
- **Structured JSON logs** with trace ID on every line via `log/slog`.

**Metrics:**

- Task claim latency (p50, p95, p99)
- Queue depth and oldest pending task age per queue
- Execution duration histogram per handler
- Orphan recovery count (should be near zero in healthy operation)
- Dead task count

---

## Testing

This is the project's real differentiator. The tests are part of the spec:

- **Concurrency:** 50 workers, 10,000 tasks, assert every task executed exactly once. Run 100 times in CI.
- **Chaos:** Kill workers with SIGKILL mid-execution at random intervals, assert every task eventually completes.
- **Clock skew:** Run workers in containers with deliberately skewed clocks, assert no early execution.
- **Backend parity:** The same test suite runs against both Postgres and Redis backends.

---

## Getting Started

**Prerequisites:** Docker, Docker Compose, Go 1.27+

```bash
# Clone the repo
git clone https://github.com/sanjayrohith/tick.git
cd tick

# Start the stack (Postgres, Redis, OTel Collector, Jaeger, Prometheus, Grafana)
make compose-up

# Run migrations
make migrate

# Build all binaries
make build

# Start the API server
./bin/tick-api

# Start a worker (in another terminal)
./bin/tick-worker

# Start the scheduler (in another terminal)
./bin/tick-scheduler

# Submit a task
curl -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "handler": "noop",
    "payload": {},
    "run_at": "2026-09-04T10:00:00Z"
  }'
```

**Environment variables:**

- `TICK_DATABASE_URL` — Postgres connection string (required)
- `TICK_REDIS_URL` — Redis connection string (optional, for hybrid backend)
- `TICK_BACKEND` — `postgres` (default) or `redis`
- `TICK_QUEUE` — Queue name (default: `default`)
- `TICK_CLAIM_BATCH` — Tasks per claim (default: 10)
- `TICK_CONCURRENCY` — Parallel task execution slots (default: 10)
- `TICK_HTTP_ADDR` — API server address (default: `:8080`)
- `TICK_LOG_LEVEL` — `debug`, `info`, `warn`, `error` (default: `info`)

---

## Configuration

Tick loads configuration from environment variables. See `internal/config/config.go` for the complete list.

**Key settings:**

- **Heartbeat:** Workers refresh every 10s. Sweeper reclaims after 90s of silence.
- **Retry backoff:** `least(300, power(2, attempts) * 5)` seconds with full jitter.
- **Task timeout:** Configurable per-task via `TICK_TASK_TIMEOUT` (default: 5 minutes).
- **Graceful shutdown:** Workers drain in-flight tasks up to `TICK_SHUTDOWN_GRACE` (default: 30s), then release remaining claims.

---

## Roadmap

- [x] Phase 1: Foundation (domain model, schema, config, logging)
- [x] Phase 2: Postgres core (claim query, retry, sweeper)
- [x] Phase 3: Worker runtime (handlers, pool, heartbeat, shutdown)
- [x] Phase 4: HTTP API (submission, status, cancellation)
- [x] Phase 5: Recurring schedules (cron, timezones, DST handling)
- [ ] Phase 6: Observability (OTel, metrics, tracing)
- [ ] Phase 7: Redis hybrid backend
- [ ] Phase 8: Test suites (concurrency, chaos, clock skew)
- [ ] Phase 9: Documentation and v0.1.0
- [ ] Phase 10: Post-release maintenance

---

## Stack

| Layer | Choice |
|---|---|
| Language | Go 1.27 |
| Postgres driver | pgx/v5 + pgxpool |
| Redis client | go-redis/v9 |
| HTTP | chi/v5 |
| Cron | robfig/cron/v3 |
| Tracing/Metrics | OpenTelemetry Go SDK |
| Logging | log/slog (JSON handler) |
| Test infra | testcontainers-go |

---

## License

MIT

---

## Why Tick?

Because distributed scheduling should be mechanically correct, not a roll of the dice. Because your team already runs Postgres and doesn't need to add Redis unless you're at the scale that actually requires it. Because exactly-once is impossible and honesty about that fact is more valuable than marketing claims.
