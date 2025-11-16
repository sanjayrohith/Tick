package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) getQueueStats(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "name: is required")
		return
	}

	stats, err := s.store.QueueStats(r.Context(), name)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "getting queue stats failed")
		return
	}

	writeJSON(w, http.StatusOK, stats)
}
