package handler

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/middleware"
	"github.com/mcchukwu/egentop/internal/org"
	"github.com/mcchukwu/egentop/internal/response"
)

type OrgHandler struct {
	OrgService *org.OrgService
}

func NewOrgHandler(service *org.OrgService) *OrgHandler {
	return &OrgHandler{
		OrgService: service,
	}
}

func (h *OrgHandler) CreateOrgs(w http.ResponseWriter, r *http.Request) {
	var req org.CreateOrganizationRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// TODO: Validate request properly
	if req.Name == "" || req.Slug == "" {
		response.HandleError(w, apperrors.ErrValidation)
		return
	}

	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, err := h.OrgService.CreateOrg(r.Context(), req.Name, req.Slug, userID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusCreated, "organization created", map[string]any{
		"organization_id": orgID,
	})
}

func (h *OrgHandler) GetOrgs(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(string)
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgs, err := h.OrgService.GetUserOrg(r.Context(), userID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "organizations fetched", orgs)
}

func (h *OrgHandler) GetOrgMembers(w http.ResponseWriter, r *http.Request) {

	org := middleware.GetOrganization(r.Context())

	members, err := h.OrgService.GetOrgMembers(r.Context(), org.ID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "members fetched", members)
}
