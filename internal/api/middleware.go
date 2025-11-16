package api

import (
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// setRequestIDHeader echoes the request ID chi's RequestID middleware placed
// on the context back onto the response, so a caller can correlate a support
// ticket with a specific log line without having to parse the body.
func setRequestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := chimw.GetReqID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}
		next.ServeHTTP(w, r)
	})
}

// recoverer turns a panic in a handler into a 500 response instead of a
// crashed process, logging the panic value so it is not silently swallowed.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			defer func() {
				if rec := recover(); rec != nil {
					log.ErrorContext(ctx, "panic recovered",
						"error", rec, "request_id", chimw.GetReqID(ctx))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"internal server error"}}`))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger emits one structured log line per request: method, path,
// status, response size and duration, tied to the request ID so it can be
// joined against the panic and error logs the same request may have produced.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.InfoContext(r.Context(), "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", chimw.GetReqID(r.Context()),
			)
		})
	}
}

// maxBodyBytes rejects a request body larger than n, so decoding a task
// submission cannot be used to exhaust process memory.
func maxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}
