package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

// listSchedules returns every schedule, enabled or not: a client managing
// its recurring work needs to see a disabled schedule too, not just have it
// silently vanish from the list.
func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	schedules, err := s.store.ListSchedules(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "listing schedules failed")
		return
	}
	if schedules == nil {
		schedules = []*domain.Schedule{}
	}
	writeJSON(w, http.StatusOK, schedules)
}

func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := scheduleIDParam(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id: must be an integer")
		return
	}

	sc, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		writeStoreError(w, err, "getting schedule failed")
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

// scheduleUpdate is the request body for PATCH /schedules/:id. Enabled is a
// pointer so an absent field is distinguishable from an explicit false --
// the only mutation this endpoint supports today is enabling or disabling,
// and a client that omits the field entirely should get a validation error
// rather than an accidental disable.
type scheduleUpdate struct {
	Enabled *bool `json:"enabled"`
}

// updateSchedule enables or disables a schedule. Disabling stops future
// materialization; it deliberately leaves every task already materialized
// from this schedule untouched, because those tasks were already promised.
func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id, err := scheduleIDParam(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "id: must be an integer")
		return
	}

	var upd scheduleUpdate
	if err = json.NewDecoder(r.Body).Decode(&upd); err != nil {
		writeJSONError(w, http.StatusBadRequest, "decoding request body failed")
		return
	}
	if upd.Enabled == nil {
		writeValidationError(w, []fieldError{{Field: "enabled", Message: "is required"}})
		return
	}

	sc, err := s.store.SetScheduleEnabled(r.Context(), id, *upd.Enabled)
	if err != nil {
		writeStoreError(w, err, "updating schedule failed")
		return
	}
	writeJSON(w, http.StatusOK, sc)
}

func scheduleIDParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
