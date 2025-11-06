-- 0001_tasks: the tasks table.
--
-- One row is one execution. Recurring work lives in the schedules table
-- (migration 0002) and is materialized into rows here.
--
-- The schedule_id foreign key and every index arrive in 0002, because schedules
-- does not exist yet and an index is cheaper to create on an empty table anyway.

CREATE TABLE tasks (
  id              BIGSERIAL PRIMARY KEY,

  -- Queues partition work so a slow queue cannot starve a fast one. Workers
  -- claim from exactly one queue.
  queue           TEXT NOT NULL DEFAULT 'default',

  -- The registered handler name. Tick never executes arbitrary code; it
  -- dispatches by name to a function the worker registered at startup.
  handler         TEXT NOT NULL,

  -- Opaque to Tick, passed to the handler untouched. JSONB rather than TEXT so
  -- operators can query into payloads when triaging a dead task.
  payload         JSONB NOT NULL,

  -- Higher is claimed first. Ties break by run_at.
  priority        INT NOT NULL DEFAULT 0,

  status          TEXT NOT NULL DEFAULT 'pending',

  -- The earliest instant this task may be claimed. Always compared against
  -- now(), the database clock, never a worker's clock: a node 200ms ahead would
  -- otherwise fire early, and with sub-minute schedules that is a real ordering
  -- bug rather than a rounding curiosity.
  run_at          TIMESTAMPTZ NOT NULL,

  -- Incremented by the claim statement itself, not by the worker on completion.
  -- A task that is claimed and then orphaned has still consumed an attempt,
  -- which is what stops a task that reliably kills its worker from looping
  -- forever through the sweeper.
  attempts        INT NOT NULL DEFAULT 0,
  max_attempts    INT NOT NULL DEFAULT 5,

  -- Worker identity holding the claim, and when it last proved it was alive.
  -- A running row whose heartbeat_at falls behind the TTL is orphaned and gets
  -- reclaimed by the sweeper.
  claimed_by      TEXT,
  heartbeat_at    TIMESTAMPTZ,

  -- Deduplicates submissions within a queue. For a materialized task this is
  -- "schedule_id:run_at", which is what stops two scheduler nodes from
  -- generating the same fire time twice. The unique index lands in 0002.
  idempotency_key TEXT,

  -- Links a materialized task back to the rule that created it. The foreign key
  -- constraint lands in 0002, once schedules exists.
  schedule_id     BIGINT,

  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

  -- Guard rails. These are cheap on write and turn a class of application bug
  -- into a loud constraint violation rather than a task that silently never
  -- runs or retries forever.
  CONSTRAINT tasks_status_valid CHECK (
    status IN ('pending', 'running', 'succeeded', 'failed', 'dead', 'cancelled')
  ),
  CONSTRAINT tasks_attempts_non_negative CHECK (attempts >= 0),
  CONSTRAINT tasks_max_attempts_positive CHECK (max_attempts > 0),
  CONSTRAINT tasks_handler_not_blank CHECK (handler <> ''),
  CONSTRAINT tasks_queue_not_blank CHECK (queue <> ''),

  -- A running task must name its owner and carry a heartbeat, or the sweeper
  -- cannot reason about whether it is alive.
  CONSTRAINT tasks_running_is_owned CHECK (
    status <> 'running' OR (claimed_by IS NOT NULL AND heartbeat_at IS NOT NULL)
  )
);

COMMENT ON TABLE tasks IS
  'One row per execution. Claimed with FOR UPDATE SKIP LOCKED; see 0002 for the supporting indexes.';

COMMENT ON COLUMN tasks.run_at IS
  'Earliest claimable instant, always compared against the database clock via now().';

COMMENT ON COLUMN tasks.attempts IS
  'Incremented by the claim statement, so an orphaned task still consumes an attempt.';

COMMENT ON COLUMN tasks.idempotency_key IS
  'Unique per queue. For materialized tasks: schedule_id:run_at.';
