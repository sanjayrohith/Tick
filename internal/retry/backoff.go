// Package retry computes the delay before a failed task is retried.
//
// The curve is capped exponential backoff with full jitter: it grows fast
// enough that a persistently failing dependency stops getting hammered, caps
// low enough that a transient blip still recovers within minutes, and jitters
// enough that a thundering herd of tasks that failed at the same instant does
// not retry in the same instant too.
package retry

import (
	"math"
	"math/rand"
	"time"
)

// Config tunes the backoff curve.
type Config struct {
	// Base is the multiplier in base * 2^attempts, in seconds.
	Base float64
	// Cap is the maximum delay, in seconds, regardless of attempts.
	Cap float64
}

// DefaultConfig matches the PRD: least(300, power(2, attempts) * 5) seconds.
func DefaultConfig() Config {
	return Config{Base: 5, Cap: 300}
}

// Ceiling returns the capped exponential value for attempts, in seconds,
// before jitter is applied: least(Cap, Base * 2^attempts). Backoff's actual
// return value is uniformly distributed between zero and this.
func (c Config) Ceiling(attempts int) float64 {
	if attempts < 0 {
		attempts = 0
	}
	return math.Min(c.Cap, math.Pow(2, float64(attempts))*c.Base)
}

// Backoff returns the delay before the next retry for the given attempt
// count, with full jitter applied: the result is uniformly distributed
// between zero and the capped exponential value, which is what stops many
// tasks that failed at once from retrying at once too.
func (c Config) Backoff(attempts int) time.Duration {
	jittered := rand.Float64() * c.Ceiling(attempts)
	return time.Duration(jittered * float64(time.Second))
}

// Backoff computes the delay for attempts using DefaultConfig. Most callers
// want this; Config.Backoff exists for callers that need a non-default curve.
func Backoff(attempts int) time.Duration {
	return DefaultConfig().Backoff(attempts)
}
