package api

import "net/http"

// healthz reports process liveness: if this handler runs at all, the process
// is up and its goroutines are scheduling. It never touches the store, so a
// database outage does not make a live process look dead.
func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz reports whether the process can actually serve traffic: it pings
// the store, so a load balancer stops routing to an instance that is up but
// cannot reach Postgres.
func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", "store is not reachable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
