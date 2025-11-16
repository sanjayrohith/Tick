package api

import (
	"errors"
	"net/http"

	"github.com/sanjayrohith/tick/internal/domain"
)

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.Cancel(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			writeJSONError(w, http.StatusNotFound, "task not found")
		case errors.Is(err, domain.ErrNotCancellable):
			writeJSONError(w, http.StatusConflict, "task is running or already terminal and cannot be cancelled")
		default:
			writeJSONError(w, http.StatusInternalServerError, "cancelling task failed")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
