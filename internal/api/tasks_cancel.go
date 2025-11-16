package api

import (
	"net/http"
)

func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.Cancel(r.Context(), id); err != nil {
		writeStoreError(w, err, "cancelling task failed")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
