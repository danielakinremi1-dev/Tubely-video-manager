package api

import (
	"context"
	"net/http"
	"time"
)

// handlerHealthz reports whether the server can do its job, for load
// balancers, container orchestrators and uptime checks.
//
// It checks the database, which is local and cheap to ping. It deliberately
// doesn't call S3: probes run every few seconds, and a brief storage outage
// would mark every instance unhealthy and get them restarted, which doesn't
// fix S3.
func (s *Server) handlerHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		respondWithError(w, http.StatusServiceUnavailable, "Database unavailable", err)
		return
	}
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
