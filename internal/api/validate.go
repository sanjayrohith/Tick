package api

import (
	"regexp"
	"strconv"
	"time"

	"github.com/sanjayrohith/tick/internal/cron"
)

// handlerNamePattern is the shape a registered handler name must take:
// letters, digits, underscore, dot and hyphen, starting with a letter. It is
// deliberately generous -- the API process does not know which handlers a
// worker fleet has actually registered, so this catches typos and garbage
// input without pretending to validate against a live registry.
var handlerNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_.-]*$`)

// priorityMin and priorityMax bound the priority field. The claim query
// orders by priority DESC, so an unbounded value lets one bad submission push
// every other task in the queue to the back indefinitely.
const (
	priorityMin = -1000
	priorityMax = 1000
)

// maxAttemptsMin and maxAttemptsMax bound max_attempts. Below 1 a task could
// never succeed; above 100 a handler that reliably fails would retry for an
// unreasonable amount of time before finally landing in dead.
const (
	maxAttemptsMin = 1
	maxAttemptsMax = 100
)

// fieldError describes one invalid or missing field in a request body.
type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// validateSubmission checks a taskSubmission's field-level constraints,
// returning every problem found rather than stopping at the first, so a
// client can fix a bad submission in one round trip instead of several.
func validateSubmission(sub taskSubmission) []fieldError {
	var errs []fieldError

	if sub.Handler == "" {
		errs = append(errs, fieldError{Field: "handler", Message: "is required"})
	} else if !handlerNamePattern.MatchString(sub.Handler) {
		errs = append(errs, fieldError{Field: "handler",
			Message: "must start with a letter and contain only letters, digits, '_', '.' or '-'"})
	}

	if len(sub.Payload) > 0 && !isJSONObject(sub.Payload) {
		errs = append(errs, fieldError{Field: "payload", Message: "must be a JSON object"})
	}

	if sub.Priority < priorityMin || sub.Priority > priorityMax {
		errs = append(errs, fieldError{Field: "priority",
			Message: rangeMessage(priorityMin, priorityMax)})
	}

	if sub.MaxAttempts != 0 && (sub.MaxAttempts < maxAttemptsMin || sub.MaxAttempts > maxAttemptsMax) {
		errs = append(errs, fieldError{Field: "max_attempts",
			Message: rangeMessage(maxAttemptsMin, maxAttemptsMax)})
	}

	if sub.RunAt != nil {
		if _, err := time.Parse(time.RFC3339, *sub.RunAt); err != nil {
			errs = append(errs, fieldError{Field: "run_at",
				Message: "must be an RFC3339 timestamp"})
		}
	}

	return errs
}

// isJSONObject reports whether raw is a JSON value whose outermost form is an
// object, without fully decoding it -- callers only need the shape, not the
// contents, and payload is opaque to the API besides that one check.
func isJSONObject(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

func rangeMessage(minValue, maxValue int) string {
	return "must be between " + strconv.Itoa(minValue) + " and " + strconv.Itoa(maxValue)
}

// validateScheduleSubmission checks a scheduleSubmission's field-level
// constraints, including parsing the cron expression and loading the
// timezone -- both are rejected at write time here, rather than being
// discovered by a materializer loop after the schedule is already live.
func validateScheduleSubmission(sub scheduleSubmission) []fieldError {
	var errs []fieldError

	if sub.Cron == "" {
		errs = append(errs, fieldError{Field: "cron", Message: "is required"})
	} else if _, err := cron.Parse(sub.Cron); err != nil {
		errs = append(errs, fieldError{Field: "cron", Message: err.Error()})
	}

	if sub.Timezone != "" {
		if _, err := time.LoadLocation(sub.Timezone); err != nil {
			errs = append(errs, fieldError{Field: "timezone",
				Message: "must be a loadable IANA location name"})
		}
	}

	if sub.Handler == "" {
		errs = append(errs, fieldError{Field: "handler", Message: "is required"})
	} else if !handlerNamePattern.MatchString(sub.Handler) {
		errs = append(errs, fieldError{Field: "handler",
			Message: "must start with a letter and contain only letters, digits, '_', '.' or '-'"})
	}

	if len(sub.Payload) > 0 && !isJSONObject(sub.Payload) {
		errs = append(errs, fieldError{Field: "payload", Message: "must be a JSON object"})
	}

	if sub.Priority < priorityMin || sub.Priority > priorityMax {
		errs = append(errs, fieldError{Field: "priority",
			Message: rangeMessage(priorityMin, priorityMax)})
	}

	if sub.MaxAttempts != 0 && (sub.MaxAttempts < maxAttemptsMin || sub.MaxAttempts > maxAttemptsMax) {
		errs = append(errs, fieldError{Field: "max_attempts",
			Message: rangeMessage(maxAttemptsMin, maxAttemptsMax)})
	}

	return errs
}
