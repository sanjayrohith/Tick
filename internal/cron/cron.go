// Package cron computes timezone- and DST-aware fire times for recurring schedules.
package cron

import (
	"fmt"
	"strings"

	robfigcron "github.com/robfig/cron/v3"

	"github.com/sanjayrohith/tick/internal/domain"
)

// parser accepts standard five-field expressions: minute hour dom month dow.
var parser = robfigcron.NewParser(
	robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow,
)

// descriptorParser additionally accepts "@every" and the other "@" shorthands
// robfig/cron understands, such as "@daily" and "@hourly".
var descriptorParser = robfigcron.NewParser(
	robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow | robfigcron.Descriptor,
)

// fieldNames names the five standard fields in order, used to report which
// one is invalid.
var fieldNames = [5]string{"minute", "hour", "day-of-month", "month", "day-of-week"}

// Schedule computes fire times for a parsed cron expression.
type Schedule struct {
	spec robfigcron.Schedule
	expr string
}

// Parse validates a five-field cron expression or an "@every" shorthand and
// returns a Schedule that computes fire times from it. The error names the
// offending field when one can be isolated, rather than only quoting the
// expression as a whole.
func Parse(expr string) (*Schedule, error) {
	trimmed := strings.TrimSpace(expr)

	if strings.HasPrefix(trimmed, "@") {
		spec, err := descriptorParser.Parse(trimmed)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", domain.ErrInvalidCron, expr, err)
		}
		return &Schedule{spec: spec, expr: expr}, nil
	}

	fields := strings.Fields(trimmed)
	if len(fields) != 5 {
		return nil, fmt.Errorf("%w: %q: expected 5 fields (%s), got %d",
			domain.ErrInvalidCron, expr, strings.Join(fieldNames[:], " "), len(fields))
	}

	spec, err := parser.Parse(trimmed)
	if err != nil {
		if field, fieldErr := locateBadField(fields); fieldErr != nil {
			return nil, fmt.Errorf("%w: %q: invalid %s field: %w", domain.ErrInvalidCron, expr, field, fieldErr)
		}
		return nil, fmt.Errorf("%w: %q: %w", domain.ErrInvalidCron, expr, err)
	}
	return &Schedule{spec: spec, expr: expr}, nil
}

// locateBadField isolates which of five space-separated fields fails to
// parse on its own, by substituting it into an otherwise-wildcard expression.
// It returns a nil error when every field parses alone, meaning the original
// failure came from field combinations rather than any single field.
func locateBadField(fields []string) (string, error) {
	for i, name := range fieldNames {
		trial := [5]string{"*", "*", "*", "*", "*"}
		trial[i] = fields[i]
		if _, err := parser.Parse(strings.Join(trial[:], " ")); err != nil {
			return name, err
		}
	}
	return "", nil
}

// String returns the original expression this Schedule was parsed from.
func (s *Schedule) String() string {
	return s.expr
}
