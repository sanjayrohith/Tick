package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// minimalEnv is the smallest environment that must produce a valid Config.
func minimalEnv() map[string]string {
	return map[string]string{
		"TICK_DATABASE_URL": "postgres://tick@localhost:5432/tick",
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(MapLookup(minimalEnv()))
	if err != nil {
		t.Fatalf("Load() with only a database URL must succeed, got: %v", err)
	}

	if cfg.Backend != BackendPostgres {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendPostgres)
	}
	if cfg.Queue != defaultQueue {
		t.Errorf("Queue = %q, want %q", cfg.Queue, defaultQueue)
	}
	if cfg.ClaimBatch != defaultClaimBatch {
		t.Errorf("ClaimBatch = %d, want %d", cfg.ClaimBatch, defaultClaimBatch)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, defaultLogLevel)
	}
}

func TestLoadReadsEveryField(t *testing.T) {
	cfg, err := Load(MapLookup(map[string]string{
		"TICK_DATABASE_URL": "postgres://localhost/tick",
		"TICK_REDIS_URL":    "redis://localhost:6379/0",
		"TICK_BACKEND":      "redis",
		"TICK_QUEUE":        "emails",
		"TICK_CLAIM_BATCH":  "64",
		"TICK_HTTP_ADDR":    "127.0.0.1:9000",
		"TICK_LOG_LEVEL":    "debug",

		"TICK_SWEEP_INTERVAL":        "45s",
		"TICK_HEARTBEAT_INTERVAL":    "15s",
		"TICK_HEARTBEAT_TTL":         "60s",
		"TICK_MATERIALIZER_INTERVAL": "20s",
		"TICK_MATERIALIZER_HORIZON":  "10m",
	}))
	if err != nil {
		t.Fatalf("Load() = %v, want success", err)
	}

	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Backend", cfg.Backend, BackendRedis},
		{"Queue", cfg.Queue, "emails"},
		{"ClaimBatch", cfg.ClaimBatch, 64},
		{"HTTPAddr", cfg.HTTPAddr, "127.0.0.1:9000"},
		{"LogLevel", cfg.LogLevel, slog.LevelDebug},
		{"RedisURL", cfg.RedisURL, "redis://localhost:6379/0"},
		{"SweepInterval", cfg.SweepInterval, 45 * time.Second},
		{"HeartbeatInterval", cfg.HeartbeatInterval, 15 * time.Second},
		{"HeartbeatTTL", cfg.HeartbeatTTL, 60 * time.Second},
		{"MaterializerInterval", cfg.MaterializerInterval, 20 * time.Second},
		{"MaterializerHorizon", cfg.MaterializerHorizon, 10 * time.Minute},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoadMissingDatabaseURL(t *testing.T) {
	_, err := Load(MapLookup(map[string]string{}))
	if err == nil {
		t.Fatal("Load() with an empty environment must fail")
	}
	assertFieldsFailed(t, err, "TICK_DATABASE_URL")
}

// The whole point of the aggregate error: one restart reveals every problem.
func TestLoadReportsAllProblemsAtOnce(t *testing.T) {
	_, err := Load(MapLookup(map[string]string{
		"TICK_BACKEND":     "mysql",
		"TICK_CLAIM_BATCH": "banana",
		"TICK_LOG_LEVEL":   "shouty",
		// TICK_DATABASE_URL deliberately absent.
	}))
	if err == nil {
		t.Fatal("Load() must fail on four bad variables")
	}
	assertFieldsFailed(t, err,
		"TICK_BACKEND", "TICK_CLAIM_BATCH", "TICK_DATABASE_URL", "TICK_LOG_LEVEL")
}

func TestLoadRedisBackendRequiresRedisURL(t *testing.T) {
	env := minimalEnv()
	env["TICK_BACKEND"] = "redis"

	_, err := Load(MapLookup(env))
	if err == nil {
		t.Fatal("the redis backend must not start without TICK_REDIS_URL")
	}
	assertFieldsFailed(t, err, "TICK_REDIS_URL")

	// The same environment is valid the moment a Redis URL appears.
	env["TICK_REDIS_URL"] = "redis://localhost:6379"
	if _, err := Load(MapLookup(env)); err != nil {
		t.Fatalf("Load() = %v, want success once TICK_REDIS_URL is set", err)
	}
}

func TestClaimBatchBounds(t *testing.T) {
	for _, tc := range []struct {
		value  string
		wantOK bool
	}{
		{"1", true},
		{"1000", true},
		{"0", false},
		{"-5", false},
		{"1001", false},
		{"1.5", false},
		{"", true}, // empty is treated as unset, so the default applies
	} {
		env := minimalEnv()
		env["TICK_CLAIM_BATCH"] = tc.value

		_, err := Load(MapLookup(env))
		if gotOK := err == nil; gotOK != tc.wantOK {
			t.Errorf("TICK_CLAIM_BATCH=%q accepted=%v, want %v (err=%v)",
				tc.value, gotOK, tc.wantOK, err)
		}
	}
}

// An empty value means unset, not "override with empty".
func TestEmptyValueFallsBackToDefault(t *testing.T) {
	env := minimalEnv()
	env["TICK_QUEUE"] = "   "

	cfg, err := Load(MapLookup(env))
	if err != nil {
		t.Fatalf("Load() = %v, want success", err)
	}
	if cfg.Queue != defaultQueue {
		t.Errorf("Queue = %q, want the default %q", cfg.Queue, defaultQueue)
	}
}

func TestBackendIsCaseInsensitive(t *testing.T) {
	env := minimalEnv()
	env["TICK_BACKEND"] = "PostgreS"

	cfg, err := Load(MapLookup(env))
	if err != nil {
		t.Fatalf("Load() = %v, want success", err)
	}
	if cfg.Backend != BackendPostgres {
		t.Errorf("Backend = %q, want %q", cfg.Backend, BackendPostgres)
	}
}

// Connection strings carry passwords and must never reach a log line.
func TestStringRedactsConnectionStrings(t *testing.T) {
	cfg, err := Load(MapLookup(map[string]string{
		"TICK_DATABASE_URL": "postgres://tick:hunter2@db.internal:5432/tick",
		"TICK_REDIS_URL":    "redis://:s3cret@cache.internal:6379",
		"TICK_BACKEND":      "redis",
	}))
	if err != nil {
		t.Fatalf("Load() = %v, want success", err)
	}

	got := cfg.String()
	for _, secret := range []string{"hunter2", "s3cret", "db.internal", "cache.internal"} {
		if strings.Contains(got, secret) {
			t.Errorf("Config.String() leaked %q:\n%s", secret, got)
		}
	}
	if !strings.Contains(got, "queue=default") {
		t.Errorf("Config.String() should still report non-secret fields:\n%s", got)
	}
}

func TestHeartbeatDefaultsSatisfyTheSafetyRatio(t *testing.T) {
	cfg, err := Load(MapLookup(minimalEnv()))
	if err != nil {
		t.Fatalf("Load() = %v, want success", err)
	}
	if cfg.SweepInterval != defaultSweepInterval {
		t.Errorf("SweepInterval = %v, want %v", cfg.SweepInterval, defaultSweepInterval)
	}
	if cfg.HeartbeatInterval != defaultHeartbeatInterval {
		t.Errorf("HeartbeatInterval = %v, want %v", cfg.HeartbeatInterval, defaultHeartbeatInterval)
	}
	if cfg.HeartbeatTTL != defaultHeartbeatTTL {
		t.Errorf("HeartbeatTTL = %v, want %v", cfg.HeartbeatTTL, defaultHeartbeatTTL)
	}
	if cfg.HeartbeatTTL < minHeartbeatTTLRatio*cfg.HeartbeatInterval {
		t.Errorf("defaults violate their own safety ratio: TTL=%v interval=%v", cfg.HeartbeatTTL, cfg.HeartbeatInterval)
	}
}

func TestHeartbeatTTLBelowSafetyRatioFails(t *testing.T) {
	env := minimalEnv()
	env["TICK_HEARTBEAT_INTERVAL"] = "10s"
	env["TICK_HEARTBEAT_TTL"] = "20s" // only 2x, the ratio requires 3x

	_, err := Load(MapLookup(env))
	if err == nil {
		t.Fatal("Load() with TTL below 3x the heartbeat interval must fail")
	}
	assertFieldsFailed(t, err, "TICK_HEARTBEAT_TTL")

	// The same environment is valid the moment the ratio is satisfied.
	env["TICK_HEARTBEAT_TTL"] = "30s"
	if _, err := Load(MapLookup(env)); err != nil {
		t.Fatalf("Load() = %v, want success once the ratio is satisfied", err)
	}
}

func TestDurationFieldRejectsGarbage(t *testing.T) {
	env := minimalEnv()
	env["TICK_SWEEP_INTERVAL"] = "banana"

	_, err := Load(MapLookup(env))
	if err == nil {
		t.Fatal("Load() with a malformed duration must fail")
	}
	assertFieldsFailed(t, err, "TICK_SWEEP_INTERVAL")
}

// assertFieldsFailed checks that err is a *ValidationError naming exactly the
// given keys.
func assertFieldsFailed(t *testing.T, err error, wantKeys ...string) {
	t.Helper()

	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is %T, want *ValidationError: %v", err, err)
	}

	got := make([]string, 0, len(ve.Fields))
	for _, f := range ve.Fields {
		got = append(got, f.Key)
		if f.Problem == "" {
			t.Errorf("field %q has an empty problem description", f.Key)
		}
	}

	if len(got) != len(wantKeys) {
		t.Fatalf("failed keys = %v, want %v", got, wantKeys)
	}
	// Load sorts problems by key, so a sorted comparison is exact.
	for i := range wantKeys {
		if got[i] != wantKeys[i] {
			t.Errorf("failed keys = %v, want %v", got, wantKeys)
			break
		}
	}
}
