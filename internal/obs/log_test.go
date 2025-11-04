package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// decode reads the single JSON object written to buf.
func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log line was written")
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("expected exactly one line, got:\n%s", line)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, line)
	}
	return rec
}

func TestLoggerEmitsJSONWithServiceAndVersion(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(LoggerOptions{
		Level:   slog.LevelInfo,
		Service: "tick-worker",
		Version: "v0.1.0",
		Output:  &buf,
	})

	log.Info("claimed tasks", "count", 7)

	rec := decode(t, &buf)
	for key, want := range map[string]any{
		"service": "tick-worker",
		"version": "v0.1.0",
		"msg":     "claimed tasks",
		"level":   "info",
		"count":   float64(7),
	} {
		if got := rec[key]; got != want {
			t.Errorf("field %q = %v, want %v", key, got, want)
		}
	}
	if _, ok := rec["ts"]; !ok {
		t.Errorf("record is missing the ts field: %v", rec)
	}
}

func TestLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(LoggerOptions{Level: slog.LevelWarn, Output: &buf})

	log.Debug("suppressed")
	log.Info("suppressed")
	if buf.Len() != 0 {
		t.Fatalf("records below the threshold must be dropped, got: %s", buf.String())
	}

	log.Warn("kept")
	if rec := decode(t, &buf); rec["level"] != "warn" {
		t.Errorf("level = %v, want warn", rec["level"])
	}
}

func TestContextAttributesAppearOnEveryRecord(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(LoggerOptions{Level: slog.LevelInfo, Output: &buf})

	ctx := With(context.Background(),
		slog.Int64("task_id", 42),
		slog.String("handler", "send_email"))

	log.InfoContext(ctx, "executing")

	rec := decode(t, &buf)
	if rec["task_id"] != float64(42) {
		t.Errorf("task_id = %v, want 42", rec["task_id"])
	}
	if rec["handler"] != "send_email" {
		t.Errorf("handler = %v, want send_email", rec["handler"])
	}
}

func TestContextAttributesAccumulate(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(LoggerOptions{Level: slog.LevelInfo, Output: &buf})

	ctx := With(context.Background(), slog.String("queue", "emails"))
	ctx = With(ctx, slog.Int64("task_id", 9))

	log.InfoContext(ctx, "executing")

	rec := decode(t, &buf)
	if rec["queue"] != "emails" || rec["task_id"] != float64(9) {
		t.Errorf("both attributes should survive, got: %v", rec)
	}
}

// A child context must not mutate its parent's attribute slice, or two
// goroutines branching from one parent contaminate each other's log lines.
func TestContextBranchesDoNotContaminate(t *testing.T) {
	parent := With(context.Background(), slog.String("queue", "emails"))
	left := With(parent, slog.Int64("task_id", 1))
	right := With(parent, slog.Int64("task_id", 2))

	for _, tc := range []struct {
		name   string
		ctx    context.Context
		wantID float64
	}{
		{"left", left, 1},
		{"right", right, 2},
	} {
		var buf bytes.Buffer
		log := NewLogger(LoggerOptions{Level: slog.LevelInfo, Output: &buf})
		log.InfoContext(tc.ctx, "executing")

		rec := decode(t, &buf)
		if rec["task_id"] != tc.wantID {
			t.Errorf("%s: task_id = %v, want %v", tc.name, rec["task_id"], tc.wantID)
		}
	}

	// The parent itself must still carry exactly one attribute.
	var buf bytes.Buffer
	log := NewLogger(LoggerOptions{Level: slog.LevelInfo, Output: &buf})
	log.InfoContext(parent, "executing")
	if rec := decode(t, &buf); rec["task_id"] != nil {
		t.Errorf("parent context leaked a child attribute: %v", rec)
	}
}

func TestLoggerIsConcurrencySafe(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	log := NewLogger(LoggerOptions{
		Level: slog.LevelInfo,
		Output: writerFunc(func(p []byte) (int, error) {
			mu.Lock()
			defer mu.Unlock()
			return buf.Write(p)
		}),
	})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			ctx := With(context.Background(), slog.Int("worker", i))
			log.InfoContext(ctx, "tick")
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != goroutines {
		t.Fatalf("wrote %d lines, want %d", len(lines), goroutines)
	}
	// Every line must be independently parseable; interleaved writes would
	// produce a corrupt object here.
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d is corrupt: %v\n%s", i, err, line)
		}
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
