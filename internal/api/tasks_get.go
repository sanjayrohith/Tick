package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/sanjayrohith/tick/internal/domain"
)

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := s.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "task not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "getting task failed")
		return
	}

	writeJSON(w, http.StatusOK, t)
}

// parseTaskID extracts and parses the ":id" URL parameter shared by every
// single-task route.
func parseTaskID(r *http.Request) (int64, error) {
	raw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errors.New("id: must be an integer")
	}
	return id, nil
}
