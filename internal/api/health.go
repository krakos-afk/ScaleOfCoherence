package api

import (
	"context"
	"net/http"
	"time"
)

// Readiness is the health surface the router needs from the database. Keeping
// it an interface lets the probes be unit-tested without a live pool.
type Readiness interface {
	Ping(ctx context.Context) error
}

// handleHealth is the liveness probe: the process is up. It never touches the
// database, so a database outage must not restart the container.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReady is the readiness probe: the process can serve traffic, which
// requires a reachable database.
func handleReady(ready Readiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := ready.Ping(ctx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
