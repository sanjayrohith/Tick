// Command tick-api is the Tick HTTP submission and inspection API.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sanjayrohith/tick/internal/api"
	"github.com/sanjayrohith/tick/internal/config"
	"github.com/sanjayrohith/tick/internal/obs"
	"github.com/sanjayrohith/tick/internal/store/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tick-api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log := obs.NewLogger(obs.LoggerOptions{Level: cfg.LogLevel, Service: "tick-api"})
	log.Info("starting tick-api", "config", cfg.String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	s, err := postgres.New(ctx, cfg.DatabaseURL, postgres.DefaultConfig())
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer func() {
		if closeErr := s.Close(); closeErr != nil {
			log.Error("closing store failed", "error", closeErr)
		}
	}()

	srv := api.New(s, log, api.DefaultConfig())
	httpSrv := srv.HTTPServer(cfg.HTTPAddr)

	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case <-ctx.Done():
		log.Info("shutting down")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serving http: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownGrace)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down http server: %w", err)
	}

	log.Info("tick-api stopped")
	return nil
}
