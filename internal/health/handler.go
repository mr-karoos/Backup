package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger abstracts database liveness checks.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Response represents the minimal, non-leaking JSON response for the health endpoint.
type Response struct {
	Status string `json:"status"`
}

// Handler serves GET /api/v1/health requests.
type Handler struct {
	db Pinger
}

// NewHandler constructs a new health check HTTP handler.
func NewHandler(db Pinger) *Handler {
	return &Handler{db: db}
}

// ServeHTTP handles incoming health probe requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	// Perform ping check with strict timeout to prevent hanging probes
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if h.db == nil || h.db.Ping(ctx) != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(Response{Status: "unavailable"})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(Response{Status: "ok"})
}
