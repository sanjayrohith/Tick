// Command tick-scheduler runs the materializer and the sweeper: it expands
// enabled schedules into tasks and reclaims tasks whose worker stopped
// heartbeating. Neither loop executes a task itself.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"github.com/sanjayrohith/tick/internal/config"
	"github.com/sanjayrohith/tick/internal/materializer"
	"github.com/sanjayrohith/tick/internal/obs"
	"github.com/sanjayrohith/tick/internal/store/postgres"
	"github.com/sanjayrohith/tick/internal/sweeper"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "tick-scheduler:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log := obs.NewLogger(obs.LoggerOptions{Level: cfg.LogLevel, Service: "tick-scheduler"})
	log.Info("starting tick-scheduler", "config", cfg.String())

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

	m := materializer.New(s, cfg.MaterializerHorizon, cfg.MaterializerInterval, log)
	sw := sweeper.New(s, cfg.SweepInterval, log, obs.NoopMetrics{})

	// Each loop runs under the same shutdown signal but on its own interval,
	// and neither depends on the other: a materializer that is slow or
	// erroring must not hold up sweeping, and vice versa. errgroup exists
	// here purely to wait for both to return once ctx is cancelled, not to
	// propagate a failure from one into cancelling the other -- Run never
	// returns an error.
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		m.Run(gCtx)
		return nil
	})
	g.Go(func() error {
		sw.Run(gCtx)
		return nil
	})

	if err := g.Wait(); err != nil {
		return err
	}
	log.Info("tick-scheduler stopped")
	return nil
}
