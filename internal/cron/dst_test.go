package cron

import (
	"testing"
	"time"
)

// TestNextNAcrossRealDSTTransitions is a table-driven pass over the DST
// policy using genuine transitions in four zones: America/New_York for the
// standard one-hour cases, Asia/Kolkata as a no-DST control that must never
// skip or dedup a fire, and Australia/Lord_Howe for the 30-minute-offset
// edge case that a naive "add or subtract one hour" implementation would get
// wrong.
func TestNextNAcrossRealDSTTransitions(t *testing.T) {
	cases := []struct {
		name  string
		zone  string
		expr  string
		after string // "2006-01-02 15:04", parsed in the case's zone
		want  []string
	}{
		{
			name:  "New York spring forward skips the nonexistent hour",
			zone:  "America/New_York",
			expr:  "30 2 * * *",
			after: "2026-03-06 00:00",
			want: []string{
				"2026-03-06 02:30",
				"2026-03-07 02:30",
				// 2026-03-08 02:30 does not exist: 02:00 EST jumps to 03:00 EDT.
				"2026-03-09 02:30",
			},
		},
		{
			name:  "New York fall back fires once, on the pre-transition offset",
			zone:  "America/New_York",
			expr:  "30 1 * * *",
			after: "2026-10-30 00:00",
			want: []string{
				"2026-10-30 01:30",
				"2026-10-31 01:30",
				// 2026-11-01 01:30 occurs twice (EDT then EST); only the first fires.
				"2026-11-01 01:30",
				"2026-11-02 01:30",
			},
		},
		{
			name:  "Kolkata never observes DST: no skip, no dedup",
			zone:  "Asia/Kolkata",
			expr:  "30 2 * * *",
			after: "2026-03-06 00:00",
			want: []string{
				"2026-03-06 02:30",
				"2026-03-07 02:30",
				"2026-03-08 02:30",
			},
		},
		{
			name:  "Lord Howe Island spring forward skips its 30-minute gap",
			zone:  "Australia/Lord_Howe",
			expr:  "15 2 * * *",
			after: "2026-10-02 00:00",
			want: []string{
				"2026-10-02 02:15",
				"2026-10-03 02:15",
				// 2026-10-04 02:15 does not exist: 02:00 +10:30 jumps to 02:30 +11.
				"2026-10-05 02:15",
			},
		},
		{
			name:  "Lord Howe Island fall back fires once across its 30-minute repeat",
			zone:  "Australia/Lord_Howe",
			expr:  "45 1 * * *",
			after: "2026-04-03 00:00",
			want: []string{
				"2026-04-03 01:45",
				"2026-04-04 01:45",
				// 2026-04-05 01:45 occurs twice (+11 then +10:30); only the first fires.
				"2026-04-05 01:45",
				"2026-04-06 01:45",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loc, err := time.LoadLocation(c.zone)
			if err != nil {
				t.Skipf("tzdata unavailable for %s: %v", c.zone, err)
			}

			after, err := time.ParseInLocation("2006-01-02 15:04", c.after, loc)
			if err != nil {
				t.Fatalf("parsing after: %v", err)
			}

			got, err := NextN(c.expr, c.zone, after, len(c.want))
			if err != nil {
				t.Fatalf("NextN() = %v, want success", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("len(got) = %d, want %d", len(got), len(c.want))
			}

			for i, w := range c.want {
				wantTime, err := time.ParseInLocation("2006-01-02 15:04", w, loc)
				if err != nil {
					t.Fatalf("parsing want[%d]: %v", i, err)
				}
				if !got[i].Equal(wantTime) {
					t.Errorf("got[%d] = %v, want %v", i, got[i].In(loc), wantTime)
				}
			}
		})
	}
}
