package api

import (
	"encoding/json"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestValidateSubmissionAcceptsValid(t *testing.T) {
	sub := taskSubmission{
		Handler:  "send_welcome",
		Payload:  json.RawMessage(`{"to":"a@example.com"}`),
		Priority: 5,
	}
	if errs := validateSubmission(sub); len(errs) != 0 {
		t.Errorf("errs = %v, want none", errs)
	}
}

func TestValidateSubmissionRequiresHandler(t *testing.T) {
	errs := validateSubmission(taskSubmission{})
	if !hasField(errs, "handler") {
		t.Errorf("errs = %v, want a handler error", errs)
	}
}

func TestValidateSubmissionRejectsBadHandlerShape(t *testing.T) {
	errs := validateSubmission(taskSubmission{Handler: "1-bad-start"})
	if !hasField(errs, "handler") {
		t.Errorf("errs = %v, want a handler error", errs)
	}
}

func TestValidateSubmissionRejectsNonObjectPayload(t *testing.T) {
	errs := validateSubmission(taskSubmission{Handler: "h", Payload: json.RawMessage(`[1,2,3]`)})
	if !hasField(errs, "payload") {
		t.Errorf("errs = %v, want a payload error", errs)
	}
}

func TestValidateSubmissionRejectsOutOfRangePriority(t *testing.T) {
	errs := validateSubmission(taskSubmission{Handler: "h", Priority: 100000})
	if !hasField(errs, "priority") {
		t.Errorf("errs = %v, want a priority error", errs)
	}
}

func TestValidateSubmissionRejectsOutOfRangeMaxAttempts(t *testing.T) {
	errs := validateSubmission(taskSubmission{Handler: "h", MaxAttempts: 1000})
	if !hasField(errs, "max_attempts") {
		t.Errorf("errs = %v, want a max_attempts error", errs)
	}
}

func TestValidateSubmissionAllowsZeroMaxAttempts(t *testing.T) {
	// Zero means "unset, use the store default", not "invalid".
	errs := validateSubmission(taskSubmission{Handler: "h"})
	if hasField(errs, "max_attempts") {
		t.Errorf("errs = %v, want no max_attempts error for the zero value", errs)
	}
}

func TestValidateSubmissionRejectsBadRunAt(t *testing.T) {
	errs := validateSubmission(taskSubmission{Handler: "h", RunAt: strPtr("not-a-timestamp")})
	if !hasField(errs, "run_at") {
		t.Errorf("errs = %v, want a run_at error", errs)
	}
}

func TestValidateSubmissionReportsEveryProblem(t *testing.T) {
	errs := validateSubmission(taskSubmission{Priority: 100000})
	if !hasField(errs, "handler") || !hasField(errs, "priority") {
		t.Errorf("errs = %v, want both handler and priority errors", errs)
	}
}

func hasField(errs []fieldError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}
