-- 0002_schedules: recurring rules, the tasks foreign key, and every index the
-- claim and sweep paths depend on.

CREATE TABLE schedules (
  id                   BIGSERIAL PRIMARY KEY,

  -- Five-field cron expression, or an @every shorthand. Validated at write time
  -- by the API so a bad expression is rejected on creation rather than
  -- discovered by a materializer loop at three in the morning.
  cron                 TEXT NOT NULL,

  -- IANA location name. A zone that observes DST makes this genuinely hard:
  -- twice a year a daily wall-clock time either does not exist or exists twice.
  -- The documented policy is skip nonexistent, fire once on ambiguous.
  timezone             TEXT NOT NULL DEFAULT 'UTC',

  -- Copied verbatim onto every task this schedule materializes.
  queue                TEXT NOT NULL DEFAULT 'default',
  handler              TEXT NOT NULL,
  payload              JSONB NOT NULL,
  priority             INT NOT NULL DEFAULT 0,
  max_attempts         INT NOT NULL DEFAULT 5,

  -- Disabling stops future materialization. It deliberately does not touch
  -- tasks already materialized: those were already promised.
  enabled              BOOLEAN NOT NULL DEFAULT true,

  -- The fire time of the most recent task generated from this schedule. The
  -- materializer resumes from here, so a scheduler node that was down for an
  -- hour catches that hour up rather than silently skipping it.
  last_materialized_at TIMESTAMPTZ,

  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT schedules_cron_not_blank CHECK (cron <> ''),
  CONSTRAINT schedules_handler_not_blank CHECK (handler <> ''),
  CONSTRAINT schedules_queue_not_blank CHECK (queue <> ''),
  CONSTRAINT schedules_timezone_not_blank CHECK (timezone <> ''),
  CONSTRAINT schedules_max_attempts_positive CHECK (max_attempts > 0)
);

COMMENT ON TABLE schedules IS
  'Recurring rules. A materializer expands these into tasks; the unique idempotency index makes concurrent materialization safe.';

COMMENT ON COLUMN schedules.last_materialized_at IS
  'Resume point for the materializer, so downtime is caught up rather than skipped.';

-- Now that schedules exists, link materialized tasks back to their rule.
-- ON DELETE SET NULL rather than CASCADE: deleting a rule must not delete the
-- history of what it already ran. The orphaned tasks keep their audit value.
ALTER TABLE tasks
  ADD CONSTRAINT tasks_schedule_id_fkey
  FOREIGN KEY (schedule_id) REFERENCES schedules(id) ON DELETE SET NULL;

-- Idempotent submission, and the thing that makes concurrent materialization a
-- no-op rather than a duplicate. Partial, because the overwhelming majority of
-- ad hoc tasks carry no key and should not pay for the index.
CREATE UNIQUE INDEX tasks_queue_idempotency_key_uniq
  ON tasks (queue, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- The claim path. Column order mirrors the claim query exactly:
--   WHERE queue = $1 AND status = 'pending' AND run_at <= now()
--   ORDER BY priority DESC, run_at
-- so the planner can satisfy both the filter and the ordering from one index
-- scan and never sort. Partial on status = 'pending' keeps the index roughly
-- the size of the backlog rather than the size of all history, which is what
-- keeps claim latency flat as the table grows into the millions.
CREATE INDEX tasks_pending_claim_idx
  ON tasks (queue, priority DESC, run_at)
  WHERE status = 'pending';

-- The sweep path: find running tasks whose heartbeat has expired. Partial on
-- status = 'running', so this index stays approximately the size of the
-- currently executing fleet, which is tiny.
CREATE INDEX tasks_running_heartbeat_idx
  ON tasks (heartbeat_at)
  WHERE status = 'running';

-- Triage: listing dead tasks per queue, and the dead task gauge.
CREATE INDEX tasks_dead_idx
  ON tasks (queue, created_at DESC)
  WHERE status = 'dead';

-- The materializer scans enabled schedules on every pass.
CREATE INDEX schedules_enabled_idx
  ON schedules (id)
  WHERE enabled;
