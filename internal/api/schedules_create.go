package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sanjayrohith/tick/internal/cron"
	"github.com/sanjayrohith/tick/internal/domain"
)

// nextFireTimesShown is how many upcoming fire times a schedule creation
// response includes, so the caller can sanity-check their expression against
// real instants immediately rather than trusting it blind until the first
// task appears.
const nextFireTimesShown = 5

// scheduleSubmission is the request body for POST /schedules.
type scheduleSubmission struct {
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Queue       string          `json:"queue"`
	Handler     string          `json:"handler"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int             `json:"priority"`
	MaxAttempts int             `json:"max_attempts"`
}

// scheduleResponse embeds the persisted schedule alongside the next fire
// times computed from it.
type scheduleResponse struct {
	*domain.Schedule
	NextFireTimes []time.Time `json:"next_fire_times"`
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	var sub scheduleSubmission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decoding request body failed")
		return
	}

	errs := validateScheduleSubmission(sub)
	if len(errs) > 0 {
		writeValidationError(w, errs)
		return
	}

	sc := &domain.Schedule{
		Cron:        sub.Cron,
		Timezone:    sub.Timezone,
		Queue:       sub.Queue,
		Handler:     sub.Handler,
		Payload:     sub.Payload,
		Priority:    sub.Priority,
		MaxAttempts: sub.MaxAttempts,
	}

	created, err := s.store.CreateSchedule(r.Context(), sc)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "creating schedule failed")
		return
	}

	// Validated above, so both are guaranteed to succeed here.
	next, _ := cron.NextN(created.Cron, created.Timezone, time.Now(), nextFireTimesShown)

	w.Header().Set("Location", fmt.Sprintf("/schedules/%d", created.ID))
	writeJSON(w, http.StatusCreated, scheduleResponse{Schedule: created, NextFireTimes: next})
}
