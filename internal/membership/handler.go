package membership

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
)

type MembershipHandler struct {
	MembershipService *MembershipService
}

func NewMembershipHandler(service *MembershipService) *MembershipHandler {
	return &MembershipHandler{
		MembershipService: service,
	}
}

// AddOrgMember adds a user to an organization
func (h *MembershipHandler) AddOrgMember(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrOrganizationNotFound)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	var req AddMemberRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// TODO: validate request properly
	if req.UserID == "" {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	err := h.MembershipService.AddOrgMember(r.Context(), organizationID, userID, req.UserID, req.Role)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusCreated, "member added", nil)
}

// GetOrgMembers returns all members of an organization
func (h *MembershipHandler) GetOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	members, err := h.MembershipService.GetOrgMembers(r.Context(), orgID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "members fetched", members)
}

// UpdateOrgMember updates a user's role in an organization
func (h *MembershipHandler) UpdateOrgMemberRole(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrOrganizationNotFound)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	targetUserID := r.PathValue("userID")

	var req UpdateMemberRoleRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	err := h.MembershipService.UpdateOrgMemberRole(r.Context(), organizationID, userID, targetUserID, req.Role)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "role updated", nil)
}

// RemoveOrgMember removes a user from an organization
func (h *MembershipHandler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrOrganizationNotFound)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	targetUserID := r.PathValue("userID")

	err := h.MembershipService.RemoveOrgMember(r.Context(), organizationID, userID, targetUserID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	response.Success(w, http.StatusOK, "member removed", nil)
}
