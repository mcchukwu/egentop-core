package organization

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type OrganizationHandler struct {
	Service *OrganizationService
}

func NewOrganizationHandler(service *OrganizationService) *OrganizationHandler {
	return &OrganizationHandler{
		Service: service,
	}
}

// Create creates a new organization
func (h *OrganizationHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var req CreateOrganizationRequest

	// Decode request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// TODO: Validate request properly
	if req.Name == "" {
		response.HandleError(w, apperrors.ErrValidation)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, err := h.Service.CreateOrganization(r.Context(), req.Name, req.Slug, userID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusCreated, "organization created", map[string]any{
		"organization_id": orgID,
	})
}

// Get returns all organizations for the authenticated user
func (h *OrganizationHandler) GetUserOrganizations(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgs, err := h.Service.GetUserOrganizations(r.Context(), userID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "organizations fetched", orgs)
}

// GetUserOrganizationByID
func (h *OrganizationHandler) GetUserOrganizationByID(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	org, err := h.Service.GetOrganizationByID(r.Context(), userID, orgID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "organization fetched", org)
}
