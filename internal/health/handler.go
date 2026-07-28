package health

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/response"
)

type HealthHandler struct {
	DB *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{
		DB: db,
	}
}

// --- Health probe ---
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	response.Success(w, http.StatusOK, "service healthy", map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

// --- Readiness probe ---
func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	err := h.DB.PingContext(ctx)

	if err != nil {
		response.Error(w, http.StatusServiceUnavailable, "service_unavailable", "database unavailable")
		return
	}

	response.Success(w, http.StatusOK, "service ready", nil)
}

// --- Liveness probe ---
func (h *HealthHandler) Live(w http.ResponseWriter, r *http.Request) {
	response.Success(w, http.StatusOK, "service alive", nil)
}

// --- ServeHTTP ---
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.Health(w, r)
	case http.MethodHead:
		h.Ready(w, r)
	case http.MethodOptions:
		h.Live(w, r)
	default:
		response.HandleError(w, apperrors.ErrMethodNotAllowed)
	}
}
