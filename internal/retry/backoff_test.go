package retry

import "testing"

func TestCeilingIsMonotonicUpToTheCap(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		attempts int
		want     float64
	}{
		{1, 10},
		{2, 20},
		{3, 40},
		{4, 80},
		{5, 160},
		{6, 300}, // 5*2^6 = 320, capped to 300
		{7, 300},
		{8, 300},
		{9, 300},
		{10, 300},
		{11, 300},
		{12, 300},
	}

	prev := 0.0
	for _, tc := range tests {
		got := cfg.Ceiling(tc.attempts)
		if got != tc.want {
			t.Errorf("Ceiling(%d) = %v, want %v", tc.attempts, got, tc.want)
		}
		if got < prev {
			t.Errorf("Ceiling(%d) = %v is less than Ceiling(%d) = %v; the curve must never shrink",
				tc.attempts, got, tc.attempts-1, prev)
		}
		if got > cfg.Cap {
			t.Errorf("Ceiling(%d) = %v exceeds the cap of %v", tc.attempts, got, cfg.Cap)
		}
		prev = got
	}
}

func TestBackoffStaysWithinJitterBounds(t *testing.T) {
	cfg := DefaultConfig()

	for attempts := 1; attempts <= 12; attempts++ {
		ceiling := cfg.Ceiling(attempts)
		for trial := 0; trial < 200; trial++ {
			d := cfg.Backoff(attempts)
			if d < 0 {
				t.Fatalf("Backoff(%d) = %v, want >= 0", attempts, d)
			}
			if d.Seconds() > ceiling {
				t.Fatalf("Backoff(%d) = %v, want <= %v seconds (the ceiling)", attempts, d, ceiling)
			}
		}
	}
}

func TestBackoffNegativeAttemptsTreatedAsZero(t *testing.T) {
	cfg := DefaultConfig()
	if got, want := cfg.Ceiling(-5), cfg.Ceiling(0); got != want {
		t.Errorf("Ceiling(-5) = %v, want Ceiling(0) = %v", got, want)
	}
}

func TestPackageLevelBackoffUsesDefaultConfig(t *testing.T) {
	d := Backoff(1)
	if d.Seconds() > DefaultConfig().Ceiling(1) {
		t.Errorf("Backoff(1) = %v, exceeds the default ceiling", d)
	}
}
