package membership

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
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

// InviteOrgMember invites a user by email to an organization
func (h *Handler) InviteOrgMember(w http.ResponseWriter, r *http.Request) {
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

	var req InviteMemberRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if len(fields) > 0 {
		response.ValidationError(w, fields)
		return
	}

	err := h.Service.InviteOrgMember(r.Context(), organizationID, userID, req.Email, req.Role)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "invitation sent", nil)
}

// AddOrgMember adds a user to an organization
func (h *Handler) AddOrgMember(w http.ResponseWriter, r *http.Request) {
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

	fields := validation.ValidateStruct(req)
	if len(fields) > 0 {
		response.ValidationError(w, fields)
		return
	}

	newMemberID, err := uuid.Parse(req.UserID)
	if err != nil {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	err = h.Service.AddOrgMember(r.Context(), organizationID, userID, newMemberID, req.Role)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "member added", nil)
}

// GetOrgMembers returns all members of an organization
func (h *Handler) GetOrgMembers(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInternalServer)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	members, meta, err := h.Service.GetOrgMembers(r.Context(), orgID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "members fetched", pagination.NewResponse(members, q, meta.Total))
}

// UpdateOrgMemberRole updates a user's role in an organization
func (h *Handler) UpdateOrgMemberRole(w http.ResponseWriter, r *http.Request) {
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

	targetUserID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	var req UpdateMemberRoleRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if len(fields) > 0 {
		response.ValidationError(w, fields)
		return
	}

	err = h.Service.UpdateOrgMemberRole(r.Context(), organizationID, userID, targetUserID, req.Role)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "role updated", nil)
}

// RemoveOrgMember removes a user from an organization
func (h *Handler) RemoveOrgMember(w http.ResponseWriter, r *http.Request) {
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

	targetUserID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrUserNotFound)
		return
	}

	err = h.Service.RemoveOrgMember(r.Context(), organizationID, userID, targetUserID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "member removed", nil)
}
