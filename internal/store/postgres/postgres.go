package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool tuning defaults. Chosen to be safe for a single small deployment; a
// production operator is expected to size MaxConns to their actual Postgres
// max_connections and worker fleet.
const (
	defaultMaxConns          = int32(20)
	defaultMinConns          = int32(2)
	defaultMaxConnLifetime   = 30 * time.Minute
	defaultHealthCheckPeriod = time.Minute
)

// pingAttempts and pingBaseDelay bound how long New waits for Postgres to
// accept connections. A `docker compose up` that starts Postgres and the app
// at the same time needs this: the app container wins the race far more often
// than it loses it, and a hard failure on the first ping would crash-loop for
// no reason.
const (
	pingAttempts  = 5
	pingBaseDelay = 200 * time.Millisecond
)

// Config tunes the pgxpool.Pool backing a Store. A zero Config is not usable
// directly; callers should start from DefaultConfig and override only what
// they need.
type Config struct {
	// MaxConns caps the pool size. Must not exceed Postgres's max_connections
	// divided by the number of processes sharing the database.
	MaxConns int32

	// MinConns keeps this many connections warm, so a burst of traffic after
	// an idle period does not pay a connection-setup latency spike.
	MinConns int32

	// MaxConnLifetime recycles connections periodically, which spreads out
	// reconnection load and lets a load balancer in front of Postgres
	// eventually route around a draining replica.
	MaxConnLifetime time.Duration

	// HealthCheckPeriod is how often pgxpool validates idle connections.
	HealthCheckPeriod time.Duration
}

// DefaultConfig returns pool tuning suitable for a single small deployment.
func DefaultConfig() Config {
	return Config{
		MaxConns:          defaultMaxConns,
		MinConns:          defaultMinConns,
		MaxConnLifetime:   defaultMaxConnLifetime,
		HealthCheckPeriod: defaultHealthCheckPeriod,
	}
}

// Store implements store.Store against Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// New builds a Store from a Postgres connection string and pool config,
// pinging the database with bounded retries and backoff before returning so
// that a cold `docker compose up` does not crash the process on its first
// connection attempt.
func New(ctx context.Context, databaseURL string, cfg Config) (*Store, error) {
	pgxCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}

	pgxCfg.MaxConns = cfg.MaxConns
	pgxCfg.MinConns = cfg.MinConns
	pgxCfg.MaxConnLifetime = cfg.MaxConnLifetime
	pgxCfg.HealthCheckPeriod = cfg.HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("building connection pool: %w", err)
	}

	if err := pingWithRetry(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	return &Store{pool: pool}, nil
}

// pingWithRetry pings the pool up to pingAttempts times with exponential
// backoff, returning the last error if none succeed.
func pingWithRetry(ctx context.Context, pool *pgxpool.Pool) error {
	var lastErr error
	for attempt := 0; attempt < pingAttempts; attempt++ {
		if attempt > 0 {
			delay := pingBaseDelay * time.Duration(1<<uint(attempt-1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("waiting to retry ping: %w", ctx.Err())
			}
		}
		if err := pool.Ping(ctx); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("pinging database after %d attempts: %w", pingAttempts, lastErr)
}

// Ping checks that the database is reachable. It backs the API's /readyz
// probe: a process that is up but cannot reach Postgres should be pulled from
// a load balancer's rotation rather than served traffic it cannot fulfil.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close releases pooled connections. Safe to call more than once.
func (s *Store) Close() error {
	if s.pool == nil {
		return nil
	}
	s.pool.Close()
	return nil
}
