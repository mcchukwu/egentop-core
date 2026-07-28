package assignment

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
)

type AssignmentHandler struct {
	Service *AssignmentService
}

func NewAssignmentHandler(service *AssignmentService) *AssignmentHandler {
	return &AssignmentHandler{
		Service: service,
	}
}

// Create creates a new assignment - /assignments
func (h *AssignmentHandler) Create(w http.ResponseWriter, r *http.Request) {
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

	projectID := r.PathValue("project_id")
	if projectID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestoneID := r.PathValue("milestone_id")
	if milestoneID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	if assignment, err := h.Service.Create(r.Context(), organizationID, userID, projectID, milestoneID, req); err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "assignment created", assignment)
}
