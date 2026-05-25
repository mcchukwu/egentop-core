package org

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type OrgHandler struct {
	OrgService *OrgService
}

func NewOrgHandler(service *OrgService) *OrgHandler {
	return &OrgHandler{
		OrgService: service,
	}
}

func (h *OrgHandler) CreateOrgs(w http.ResponseWriter, r *http.Request) {
	var req CreateOrganizationRequest

	// Decode request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// TODO: Validate request properly
	if req.Name == "" || req.Slug == "" {
		response.HandleError(w, apperrors.ErrValidation)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
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
	userID, ok := requestctx.UserID(r.Context())
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
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	members, err := h.OrgService.GetOrgMembers(r.Context(), orgID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "members fetched", members)
}
