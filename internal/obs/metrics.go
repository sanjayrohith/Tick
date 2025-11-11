package obs

// Metrics is the seam every metrics-emitting component in Tick depends on,
// rather than on a Prometheus client directly. That keeps the sweeper and
// friends compilable and testable now, with the real OpenTelemetry-backed
// implementation arriving as an additive change in the observability phase
// instead of a rework of every caller.
type Metrics interface {
	// OrphansRecovered records that the sweeper reclaimed n tasks whose
	// heartbeat had expired. Near-zero in healthy operation; a sustained
	// nonzero rate means workers are dying faster than they should.
	OrphansRecovered(n int)
}

// NoopMetrics discards everything. It is the default so that nothing in Tick
// has to nil-check a metrics dependency before using it.
type NoopMetrics struct{}

// OrphansRecovered discards n.
func (NoopMetrics) OrphansRecovered(int) {}
