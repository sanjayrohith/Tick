package cron

import (
	"errors"
	"strings"
	"testing"

	"github.com/sanjayrohith/tick/internal/domain"
)

func TestParseValidExpressions(t *testing.T) {
	cases := []string{
		"* * * * *",
		"30 2 * * *",
		"0 9-17 * * MON-FRI",
		"@every 90s",
		"@daily",
		"@hourly",
	}
	for _, expr := range cases {
		if _, err := Parse(expr); err != nil {
			t.Errorf("Parse(%q) = %v, want success", expr, err)
		}
	}
}

func TestParseWrongFieldCount(t *testing.T) {
	_, err := Parse("30 2 * *")
	if !errors.Is(err, domain.ErrInvalidCron) {
		t.Fatalf("error = %v, want it to wrap ErrInvalidCron", err)
	}
	if !strings.Contains(err.Error(), "got 4") {
		t.Errorf("error = %v, want it to report the field count found", err)
	}
}

func TestParseNamesOffendingField(t *testing.T) {
	cases := []struct {
		expr  string
		field string
	}{
		{"99 2 * * *", "minute"},
		{"30 99 * * *", "hour"},
		{"30 2 99 * *", "day-of-month"},
		{"30 2 * 99 *", "month"},
		{"30 2 * * 99", "day-of-week"},
	}
	for _, c := range cases {
		_, err := Parse(c.expr)
		if err == nil {
			t.Fatalf("Parse(%q) succeeded, want an error", c.expr)
		}
		if !errors.Is(err, domain.ErrInvalidCron) {
			t.Errorf("Parse(%q) error = %v, want it to wrap ErrInvalidCron", c.expr, err)
		}
		if !strings.Contains(err.Error(), c.field) {
			t.Errorf("Parse(%q) error = %v, want it to name field %q", c.expr, err, c.field)
		}
	}
}

func TestParseInvalidEvery(t *testing.T) {
	_, err := Parse("@every not-a-duration")
	if !errors.Is(err, domain.ErrInvalidCron) {
		t.Fatalf("error = %v, want it to wrap ErrInvalidCron", err)
	}
}
