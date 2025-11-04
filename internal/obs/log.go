// Package obs provides logging, tracing, and metrics wiring.
//
// Logs are JSON on stdout with one object per line. Nothing here formats for
// humans: these lines are meant to be shipped, indexed, and correlated with
// traces, and a log pipeline that has to parse two formats is a log pipeline
// that silently drops the one you forgot about.
package obs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// LoggerOptions configures the process logger.
type LoggerOptions struct {
	// Level is the minimum level emitted.
	Level slog.Level

	// Service names the emitting process, for example "tick-worker". It is
	// stamped on every line so a single index can separate the three binaries.
	Service string

	// Version is the build version, stamped on every line so a bad deploy is
	// visible in the logs rather than inferred from timestamps.
	Version string

	// Output defaults to os.Stdout when nil.
	Output io.Writer

	// AddSource includes the file and line that emitted the record. Useful in
	// development, noisy and measurably slower in production.
	AddSource bool
}

// NewLogger builds the process logger. The returned logger is safe for
// concurrent use by any number of goroutines.
func NewLogger(opts LoggerOptions) *slog.Logger {
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	// Typed as the interface so WithAttrs below needs no type assertion back
	// to the concrete handler.
	var handler slog.Handler = slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level:       opts.Level,
		AddSource:   opts.AddSource,
		ReplaceAttr: normalizeAttr,
	})

	attrs := make([]slog.Attr, 0, 2)
	if opts.Service != "" {
		attrs = append(attrs, slog.String("service", opts.Service))
	}
	if opts.Version != "" {
		attrs = append(attrs, slog.String("version", opts.Version))
	}
	if len(attrs) > 0 {
		handler = handler.WithAttrs(attrs)
	}

	return slog.New(&contextHandler{Handler: handler})
}

// normalizeAttr makes the output stable across Go versions and log backends:
// levels lowercase to match every other field, and the timestamp is explicitly
// named "ts" in RFC3339 with nanoseconds rather than slog's default layout.
func normalizeAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.LevelKey:
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(strings.ToLower(lvl.String()))
		}
	case slog.TimeKey:
		a.Key = "ts"
	case slog.MessageKey:
		a.Key = "msg"
	}
	return a
}

// contextHandler enriches every record with values carried on the context.
//
// Today that means the request-scoped attributes attached by With. Once tracing
// lands, this is the single place trace_id and span_id get stamped onto every
// line, which is what makes a log entry one click away from its trace.
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs := attrsFromContext(ctx); len(attrs) > 0 {
		r.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

// ctxKey is the unexported key type for context values owned by this package.
type ctxKey struct{}

// With returns a context carrying attrs, which are then stamped on every log
// record emitted with that context. Use it to attach a task ID once at the top
// of execution instead of threading it through every call site.
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing := attrsFromContext(ctx)
	// Copy rather than append in place: the parent context's slice must not be
	// mutated by a child, or sibling goroutines see each other's attributes.
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, ctxKey{}, merged)
}

func attrsFromContext(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	return attrs
}
