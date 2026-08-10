package activity

import (
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{Service: service}
}

// List returns the activity feed for the active organization
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	activities, meta, err := h.Service.List(r.Context(), orgID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "activities fetched", pagination.NewResponse(activities, q, meta.Total))
}
