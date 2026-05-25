package handler

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/org"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type MembershipHandler struct {
	OrgService *org.OrgService
}

func NewMembershipHandler(service *org.OrgService) *MembershipHandler {
	return &MembershipHandler{
		OrgService: service,
	}
}

func (h *MembershipHandler) AddOrgMember(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrOrganizationNotFound)
		return
	}

	var req org.AddMemberRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// TODO: validate request properly
	if req.UserID == "" {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	err := h.OrgService.AddOrgMember(r.Context(), organizationID, req.UserID, req.Role)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusCreated, "member added", nil)
}

func (h *MembershipHandler) UpdateOrgMemberRole(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrOrganizationNotFound)
		return
	}

	targetUserID := r.PathValue("userID")

	var req org.UpdateMemberRoleRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	err := h.OrgService.UpdateOrgMemberRole(r.Context(), organizationID, targetUserID, req.Role)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "role updated", nil)
}

func (h *MembershipHandler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrOrganizationNotFound)
		return
	}

	targetUserID := r.PathValue("userID")

	err := h.OrgService.RemoveOrgMember(r.Context(), organizationID, targetUserID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "member removed", nil)
}
