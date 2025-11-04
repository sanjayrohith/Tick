# Tick

A distributed task scheduler for teams that already run Postgres.

Running scheduled work across more than one machine is where most teams
improvise badly. Cron on a single box has no retries, no history, and no second
machine. Naive `SELECT` then `UPDATE` polling leaves a gap between read and
write, so under concurrency two workers grab the same row and run the job twice.
`SELECT ... FOR UPDATE` without `SKIP LOCKED` makes workers queue behind each
other, so adding workers adds latency instead of throughput. Tick claims tasks
with a single `UPDATE ... FOR UPDATE SKIP LOCKED` statement, keeps the database
clock as the only clock that matters, and recovers tasks orphaned by dead
workers through heartbeat expiry.

**Delivery is at-least-once.** Exactly-once delivery is not achievable across a
network boundary, and claiming otherwise is a red flag. Tick guarantees
at-least-once dispatch and gives you idempotency keys so your handler can make
the observable effect happen once. Write your handlers accordingly.

Status: early development.
