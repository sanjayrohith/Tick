// Command tick-worker is the Tick worker process: it claims tasks from one
// queue, executes them with registered handlers, and heartbeats its claims
// until they finish.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sanjayrohith/tick/internal/config"
	"github.com/sanjayrohith/tick/internal/obs"
	"github.com/sanjayrohith/tick/internal/store/postgres"
	"github.com/sanjayrohith/tick/internal/worker"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tick-worker:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log := obs.NewLogger(obs.LoggerOptions{Level: cfg.LogLevel, Service: "tick-worker"})
	log.Info("starting tick-worker", "config", cfg.String())

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

	registry := worker.NewRegistry()
	registry.Register("noop", worker.HandlerFunc(noopHandler))
	registry.Register("sleep", worker.HandlerFunc(sleepHandler))
	registry.Register("flaky", worker.HandlerFunc(flakyHandler))

	id := worker.Identity()
	log.Info("worker identity", "id", id)

	w := worker.New(s, registry, id, worker.Config{
		Queue:             cfg.Queue,
		ClaimBatch:        cfg.ClaimBatch,
		Concurrency:       cfg.Concurrency,
		HeartbeatInterval: cfg.HeartbeatInterval,
		TaskTimeout:       cfg.TaskTimeout,
		ShutdownGrace:     cfg.ShutdownGrace,
	}, log)

	w.Run(ctx)
	log.Info("tick-worker stopped")
	return nil
}
