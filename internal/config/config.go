// Package config loads and validates Tick configuration from the environment.
//
// Every setting has one source of truth: an environment variable prefixed with
// TICK_. Loading is total, meaning a call to Load either returns a fully valid
// Config or an error naming every problem at once. Reporting one missing key per
// restart turns a single misconfiguration into a slow guessing game, which is
// exactly the wrong experience at three in the morning.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Backend selects the storage implementation backing the task queue.
type Backend string

const (
	// BackendPostgres stores and claims tasks directly in Postgres. This is the
	// default and is correct for the overwhelming majority of deployments.
	BackendPostgres Backend = "postgres"

	// BackendRedis layers a Redis Streams cache of imminent work over Postgres.
	// Postgres remains the source of truth. Only worth the extra moving part
	// past roughly 10M tasks/day.
	BackendRedis Backend = "redis"
)

// Config is the fully resolved configuration for any Tick process.
type Config struct {
	// Backend selects the store implementation. TICK_BACKEND.
	Backend Backend

	// DatabaseURL is the Postgres connection string. Required for every
	// backend, because Postgres is always the source of truth. TICK_DATABASE_URL.
	DatabaseURL string

	// RedisURL is the Redis connection string. Required only when Backend is
	// BackendRedis. TICK_REDIS_URL.
	RedisURL string

	// Queue is the queue this process serves. TICK_QUEUE.
	Queue string

	// ClaimBatch is the maximum number of tasks a worker claims per round trip.
	// TICK_CLAIM_BATCH.
	ClaimBatch int

	// Concurrency bounds how many tasks a worker executes at once. Claiming is
	// throttled to this same limit, so a worker never holds more claims than
	// it has capacity to run. TICK_CONCURRENCY.
	Concurrency int

	// HTTPAddr is the listen address for the API server. TICK_HTTP_ADDR.
	HTTPAddr string

	// LogLevel is the minimum level emitted by the structured logger.
	// TICK_LOG_LEVEL.
	LogLevel slog.Level

	// SweepInterval is how often the sweeper checks for orphaned tasks.
	// TICK_SWEEP_INTERVAL.
	SweepInterval time.Duration

	// HeartbeatInterval is how often a worker refreshes heartbeat_at on its
	// claimed tasks. TICK_HEARTBEAT_INTERVAL.
	HeartbeatInterval time.Duration

	// HeartbeatTTL is how long a task may go without a heartbeat before the
	// sweeper treats it as orphaned. TICK_HEARTBEAT_TTL.
	HeartbeatTTL time.Duration

	// TaskTimeout bounds how long a single handler invocation may run before
	// the worker cancels it and reports a timeout as a retryable failure.
	// TICK_TASK_TIMEOUT.
	TaskTimeout time.Duration
}

// Defaults applied when a variable is unset. Chosen so that a developer with
// only TICK_DATABASE_URL set gets a working single-queue Postgres deployment.
const (
	defaultBackend           = BackendPostgres
	defaultQueue             = "default"
	defaultClaimBatch        = 10
	defaultConcurrency       = 10
	defaultHTTPAddr          = ":8080"
	defaultLogLevel          = slog.LevelInfo
	defaultSweepInterval     = 30 * time.Second
	defaultHeartbeatInterval = 10 * time.Second
	defaultHeartbeatTTL      = 90 * time.Second
	defaultTaskTimeout       = 5 * time.Minute
)

// maxClaimBatch bounds a single claim. A very large batch holds row locks for
// longer than a worker can plausibly heartbeat them, which converts a slow
// handler into a wave of false orphan recoveries.
const maxClaimBatch = 1000

// maxConcurrency bounds how many tasks one worker process may run at once.
// Past this a single process is doing the job of a fleet and should be split
// instead of tuned further.
const maxConcurrency = 1000

// minHeartbeatTTLRatio is the minimum multiple HeartbeatTTL must be of
// HeartbeatInterval. A ratio of 3 means a worker has to miss three
// consecutive heartbeats, not one, before the sweeper reclaims its task -- a
// single slow garbage-collection pause must not be indistinguishable from a
// dead worker.
const minHeartbeatTTLRatio = 3

// FieldError describes one invalid or missing configuration variable.
type FieldError struct {
	// Key is the environment variable name, for example "TICK_CLAIM_BATCH".
	Key string
	// Problem describes what is wrong, in lowercase and without punctuation.
	Problem string
}

func (e FieldError) Error() string { return e.Key + ": " + e.Problem }

// ValidationError aggregates every problem found during a single Load, so one
// restart surfaces the complete list rather than only the first failure.
type ValidationError struct {
	// Fields holds one entry per invalid or missing variable, sorted by key.
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	if len(e.Fields) == 1 {
		return "invalid configuration, " + e.Fields[0].Error()
	}
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, "  "+f.Error())
	}
	return fmt.Sprintf("invalid configuration, %d problems:\n%s",
		len(e.Fields), strings.Join(parts, "\n"))
}

// LookupFunc resolves an environment variable. It matches the signature of
// os.LookupEnv so tests can supply a map without touching process state.
type LookupFunc func(key string) (value string, ok bool)

// MapLookup adapts a map to a LookupFunc.
func MapLookup(env map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

// Load resolves configuration from the given lookup, applying defaults and
// validating the result. On failure it returns a *ValidationError listing every
// problem found, never only the first.
func Load(lookup LookupFunc) (*Config, error) {
	l := &loader{lookup: lookup}

	cfg := &Config{
		DatabaseURL: l.required("TICK_DATABASE_URL"),
		RedisURL:    l.optional("TICK_REDIS_URL", ""),
		Queue:       l.optional("TICK_QUEUE", defaultQueue),
		ClaimBatch:  l.intInRange("TICK_CLAIM_BATCH", defaultClaimBatch, 1, maxClaimBatch),
		Concurrency: l.intInRange("TICK_CONCURRENCY", defaultConcurrency, 1, maxConcurrency),
		HTTPAddr:    l.optional("TICK_HTTP_ADDR", defaultHTTPAddr),
		Backend:     l.backend("TICK_BACKEND", defaultBackend),
		LogLevel:    l.logLevel("TICK_LOG_LEVEL", defaultLogLevel),

		SweepInterval:     l.duration("TICK_SWEEP_INTERVAL", defaultSweepInterval),
		HeartbeatInterval: l.duration("TICK_HEARTBEAT_INTERVAL", defaultHeartbeatInterval),
		HeartbeatTTL:      l.duration("TICK_HEARTBEAT_TTL", defaultHeartbeatTTL),
		TaskTimeout:       l.duration("TICK_TASK_TIMEOUT", defaultTaskTimeout),
	}

	// Cross-field rule: the Redis backend cannot start without a Redis URL.
	// Checked here rather than in the field readers because it depends on two
	// variables at once.
	if cfg.Backend == BackendRedis && cfg.RedisURL == "" {
		l.fail("TICK_REDIS_URL", "required when TICK_BACKEND is redis")
	}

	// Cross-field rule: the TTL must give a worker room to miss more than one
	// heartbeat before the sweeper reclaims its task. A single missed beat
	// orphaning a live task is a false positive the ratio exists to prevent.
	if cfg.HeartbeatTTL < minHeartbeatTTLRatio*cfg.HeartbeatInterval {
		l.fail("TICK_HEARTBEAT_TTL", fmt.Sprintf(
			"must be at least %dx TICK_HEARTBEAT_INTERVAL (%s), got %s",
			minHeartbeatTTLRatio, cfg.HeartbeatInterval, cfg.HeartbeatTTL))
	}

	if len(l.problems) > 0 {
		sort.Slice(l.problems, func(i, j int) bool {
			return l.problems[i].Key < l.problems[j].Key
		})
		return nil, &ValidationError{Fields: l.problems}
	}
	return cfg, nil
}

// FromEnv resolves configuration from the process environment. It is the entry
// point every command uses; Load exists so tests can supply an environment
// without mutating process state.
func FromEnv() (*Config, error) {
	return Load(os.LookupEnv)
}

// loader accumulates problems while reading fields, so a single pass can report
// all of them together.
type loader struct {
	lookup   LookupFunc
	problems []FieldError
}

func (l *loader) fail(key, problem string) {
	l.problems = append(l.problems, FieldError{Key: key, Problem: problem})
}

// get returns the trimmed value and whether it was present and non-empty. A
// variable set to the empty string is treated as unset; exporting TICK_QUEUE=""
// almost always means "I meant to unset this".
func (l *loader) get(key string) (string, bool) {
	raw, ok := l.lookup(key)
	if !ok {
		return "", false
	}
	v := strings.TrimSpace(raw)
	return v, v != ""
}

func (l *loader) required(key string) string {
	v, ok := l.get(key)
	if !ok {
		l.fail(key, "is required but not set")
		return ""
	}
	return v
}

func (l *loader) optional(key, def string) string {
	if v, ok := l.get(key); ok {
		return v
	}
	return def
}

func (l *loader) intInRange(key string, def, minValue, maxValue int) int {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be an integer, got %q", v))
		return def
	}
	if n < minValue || n > maxValue {
		l.fail(key, fmt.Sprintf("must be between %d and %d, got %d", minValue, maxValue, n))
		return def
	}
	return n
}

// duration parses a Go duration string such as "30s" or "90s".
func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be a duration like \"30s\", got %q", v))
		return def
	}
	if d <= 0 {
		l.fail(key, fmt.Sprintf("must be positive, got %q", v))
		return def
	}
	return d
}

func (l *loader) backend(key string, def Backend) Backend {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	switch Backend(strings.ToLower(v)) {
	case BackendPostgres:
		return BackendPostgres
	case BackendRedis:
		return BackendRedis
	default:
		l.fail(key, fmt.Sprintf("must be %q or %q, got %q", BackendPostgres, BackendRedis, v))
		return def
	}
}

func (l *loader) logLevel(key string, def slog.Level) slog.Level {
	v, ok := l.get(key)
	if !ok {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(v)); err != nil {
		l.fail(key, fmt.Sprintf("must be one of debug, info, warn, error, got %q", v))
		return def
	}
	return lvl
}

// String renders the configuration for startup logging with no secret material.
// Connection strings carry passwords, so only their presence is reported.
func (c *Config) String() string {
	return fmt.Sprintf(
		"backend=%s queue=%s claim_batch=%d concurrency=%d http_addr=%s log_level=%s "+
			"sweep_interval=%s heartbeat_interval=%s heartbeat_ttl=%s task_timeout=%s "+
			"database_url=%s redis_url=%s",
		c.Backend, c.Queue, c.ClaimBatch, c.Concurrency, c.HTTPAddr, c.LogLevel,
		c.SweepInterval, c.HeartbeatInterval, c.HeartbeatTTL, c.TaskTimeout,
		redacted(c.DatabaseURL), redacted(c.RedisURL),
	)
}

func redacted(url string) string {
	if url == "" {
		return "<unset>"
	}
	return "<set>"
}

// ClaimTimeout is the ceiling on how long a single claim round trip may take
// before the worker gives up and retries. Derived rather than configured, so it
// cannot drift out of proportion with the batch size.
func (c *Config) ClaimTimeout() time.Duration {
	return time.Duration(c.ClaimBatch)*50*time.Millisecond + 2*time.Second
}
