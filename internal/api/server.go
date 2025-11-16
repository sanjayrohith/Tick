package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Config tunes the HTTP server's timeouts and hardening limits.
type Config struct {
	// ReadTimeout bounds how long reading a request, including its body, may
	// take.
	ReadTimeout time.Duration

	// WriteTimeout bounds how long writing a response may take.
	WriteTimeout time.Duration

	// IdleTimeout bounds how long a keep-alive connection may sit between
	// requests before the server closes it.
	IdleTimeout time.Duration

	// RequestTimeout bounds how long a single handler invocation may run
	// before it is cancelled and the client sees a timeout response.
	RequestTimeout time.Duration

	// MaxBodyBytes caps the size of a request body. A task submission has no
	// legitimate reason to be large, so this also bounds memory used decoding
	// one.
	MaxBodyBytes int64
}

// DefaultConfig returns timeouts and limits suitable for a public-facing
// submission API: generous enough for a slow client, tight enough that a
// stalled connection cannot pin a handler goroutine indefinitely.
func DefaultConfig() Config {
	return Config{
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    90 * time.Second,
		RequestTimeout: 25 * time.Second,
		MaxBodyBytes:   1 << 20, // 1 MiB; a task payload has no business being larger.
	}
}

// Server serves the Tick HTTP API.
type Server struct {
	router chi.Router
	store  Store
	log    *slog.Logger
	cfg    Config
}

// New builds a Server backed by store s, with its middleware stack wired:
// request ID, panic recovery, request logging, a per-request timeout, and a
// cap on request body size. A nil logger discards output.
func New(s Store, log *slog.Logger, cfg Config) *Server {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	srv := &Server{store: s, log: log, cfg: cfg}

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(setRequestIDHeader)
	r.Use(recoverer(log))
	r.Use(requestLogger(log))
	r.Use(chimw.Timeout(cfg.RequestTimeout))
	r.Use(maxBodyBytes(cfg.MaxBodyBytes))
	srv.router = r
	srv.routes()

	return srv
}

// Handler returns the server's routes as an http.Handler.
func (s *Server) Handler() http.Handler {
	return s.router
}

// HTTPServer builds an *http.Server bound to addr, with read/write/idle
// timeouts set so a slow or hung client cannot exhaust the process's
// goroutines or file descriptors.
func (s *Server) HTTPServer(addr string) *http.Server {
	return &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.cfg.ReadTimeout,
		WriteTimeout: s.cfg.WriteTimeout,
		IdleTimeout:  s.cfg.IdleTimeout,
	}
}
