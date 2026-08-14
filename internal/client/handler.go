package client

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/normalize"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{Service: service}
}

// Provision provisions a client account - POST /orgs/{orgID}/clients
func (h *Handler) Provision(w http.ResponseWriter, r *http.Request) {
	var req ProvisionRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	// Normalize identifiers before validation, mirroring register/login.
	if req.Phone != "" {
		normalized, err := normalize.Phone(req.Phone, "")
		if err != nil {
			response.ValidationError(w, map[string]string{"phone": "must be a valid phone number"})
			return
		}
		req.Phone = normalized
	}
	if req.Email != "" {
		req.Email = normalize.Email(req.Email)
	}

	if fields := validation.ValidateStruct(req); fields != nil {
		response.ValidationError(w, fields)
		return
	}

	if req.Email == "" && req.Phone == "" {
		response.ValidationError(w, map[string]string{"email": "email or phone is required", "phone": "email or phone is required"})
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	result, err := h.Service.Provision(r.Context(), userID, orgID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "client provisioned", result)
}

// ResetCredential rotates a client's one-time credential and revokes their
// sessions - POST /orgs/{orgID}/clients/{userID}/reset-credential
func (h *Handler) ResetCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	targetUserID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrClientNotFound)
		return
	}

	result, err := h.Service.ResetCredential(r.Context(), userID, orgID, targetUserID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "client credential reset", result)
}

// List lists the organization's clients - GET /orgs/{orgID}/clients
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	clients, meta, err := h.Service.List(r.Context(), orgID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "clients fetched", pagination.NewResponse(clients, q, meta.Total))
}

// Remove removes a provisioned-but-unassigned client's membership -
// DELETE /orgs/{orgID}/clients/{userID}
func (h *Handler) Remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	targetUserID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrClientNotFound)
		return
	}

	if err := h.Service.Remove(r.Context(), userID, orgID, targetUserID); err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "client removed", nil)
}
