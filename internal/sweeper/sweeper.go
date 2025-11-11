package sweeper

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/sanjayrohith/tick/internal/obs"
)

// DefaultInterval is how often the sweeper checks for orphaned tasks when no
// other interval is configured.
const DefaultInterval = 30 * time.Second

// Recoverer is the store surface the sweeper needs: reclaiming tasks whose
// heartbeat has gone silent. Defined here rather than depending on the store
// package's full Store interface, so the sweeper only ever asks for what it
// actually uses.
type Recoverer interface {
	RecoverOrphans(ctx context.Context) (int, error)
}

// Sweeper periodically recovers orphaned tasks.
type Sweeper struct {
	recoverer Recoverer
	interval  time.Duration
	log       *slog.Logger
	metrics   obs.Metrics
}

// New builds a Sweeper. A zero interval falls back to DefaultInterval, a nil
// logger discards output rather than panicking, and a nil metrics dependency
// falls back to obs.NoopMetrics so the sweeper works standalone before a real
// metrics backend exists.
func New(recoverer Recoverer, interval time.Duration, log *slog.Logger, metrics obs.Metrics) *Sweeper {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if metrics == nil {
		metrics = obs.NoopMetrics{}
	}
	return &Sweeper{recoverer: recoverer, interval: interval, log: log, metrics: metrics}
}

// Run sweeps on a ticker until ctx is cancelled. A recovery error is logged
// and the loop keeps going: a sweeper that dies on the first transient
// database hiccup defeats the entire point of having one.
//
// The first tick is jittered across [0, interval) rather than firing
// immediately, so that any number of scheduler nodes started at the same
// moment do not all sweep in lockstep and hammer the database together on a
// fixed 30 second beat.
func (s *Sweeper) Run(ctx context.Context) {
	firstDelay := time.Duration(rand.Int64N(int64(s.interval)))
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	s.sweepOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *Sweeper) sweepOnce(ctx context.Context) {
	recovered, err := s.recoverer.RecoverOrphans(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "sweep failed", "error", err)
		return
	}
	if recovered > 0 {
		s.log.InfoContext(ctx, "recovered orphaned tasks", "count", recovered)
		s.metrics.OrphansRecovered(recovered)
	}
}
