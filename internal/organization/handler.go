package organization

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		Service: service,
	}
}

// Create creates a new organization
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateOrganizationRequest

	// Decode request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if len(fields) > 0 {
		response.ValidationError(w, fields)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, err := h.Service.Create(r.Context(), req.Name, userID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "organization created", map[string]any{
		"organization_id": orgID,
	})
}

// Get returns all organizations for the authenticated user
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	orgs, meta, err := h.Service.List(r.Context(), userID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "organizations fetched", pagination.NewResponse(orgs, q, meta.Total))
}

// GetByID
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	org, err := h.Service.GetByID(r.Context(), orgID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "organization fetched", org)
}

// Update updates an organization's details
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateOrganizationRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if len(fields) > 0 {
		response.ValidationError(w, fields)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	if err := h.Service.Update(r.Context(), orgID, req.Name); err != nil {
		response.HandleError(w, err)
		return
	}

	org, err := h.Service.GetByID(r.Context(), orgID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "organization updated", org)
}
