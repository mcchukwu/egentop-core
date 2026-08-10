package assignment

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

// Create creates a new assignment - /assignments
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req CreateAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	if fields := validation.ValidateStruct(req); fields != nil {
		response.ValidationError(w, fields)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestoneID, err := uuid.Parse(req.MilestoneID)
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignedTo, err := uuid.Parse(req.AssignedTo)
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignment, err := h.Service.Create(r.Context(), organizationID, userID, projectID, milestoneID, assignedTo)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "assignment created", assignment)
}

// GetByID returns a single assignment scoped to the active organization
func (h *Handler) GetByID(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	assignmentID, err := uuid.Parse(r.PathValue("assignmentID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignment, err := h.Service.GetByID(r.Context(), organizationID, assignmentID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "assignment fetched", assignment)
}

// ListByProjectID lists all assignments for a project scoped to the active organization
func (h *Handler) ListByProjectID(w http.ResponseWriter, r *http.Request) {
	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	assignments, meta, err := h.Service.ListByProjectID(r.Context(), organizationID, projectID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "assignments fetched", pagination.NewResponse(assignments, q, meta.Total))
}

// Update reassigns an assignment to a new user
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateAssignmentRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	if fields := validation.ValidateStruct(req); fields != nil {
		response.ValidationError(w, fields)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignmentID, err := uuid.Parse(r.PathValue("assignmentID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignedTo, err := uuid.Parse(req.AssignedTo)
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignment, err := h.Service.Update(r.Context(), organizationID, userID, projectID, assignmentID, assignedTo)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "assignment updated", assignment)
}

// Delete removes an assignment
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	organizationID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	assignmentID, err := uuid.Parse(r.PathValue("assignmentID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	err = h.Service.Delete(r.Context(), organizationID, userID, projectID, assignmentID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "assignment removed", nil)
}
