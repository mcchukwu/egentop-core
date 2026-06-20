package project

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
)

type ProjectHandler struct {
	Service *ProjectService
}

func NewProjectHandler(service *ProjectService) *ProjectHandler {
	return &ProjectHandler{Service: service}
}

func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if fields != nil {
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

	project, err := h.Service.CreateProject(r.Context(), userID, organizationID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "project created", project)
}

func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	projects, err := h.Service.ListProjects(r.Context(), orgID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "projects fetched", projects)
}

// GetProjectByID gets a project by ID - /projects/{id}
func (h *ProjectHandler) GetProjectByID(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")

	_, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	project, err := h.Service.GetProjectByID(r.Context(), projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "project fetched", project)
}

func (h *ProjectHandler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
	var req CreateMilestoneInput

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if fields != nil {
		response.ValidationError(w, fields)
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

	projectID := req.ProjectID
	if projectID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.CreateMilestone(r.Context(), orgID, userID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "milestone created", milestone)
}

func (h *ProjectHandler) ListMilestones(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project_id")
	if projectID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	_, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestones, err := h.Service.ListMilestones(r.Context(), projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestones fetched", milestones)
	// TODO: pagination
}

func (h *ProjectHandler) UpdateMilestoneStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateMilestoneStatusInput

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	fields := validation.ValidateStruct(req)
	if fields != nil {
		response.ValidationError(w, fields)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	milestoneID := r.PathValue("milestone_id")
	if milestoneID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	err := h.Service.UpdateMilestoneStatus(r.Context(), orgID, userID, milestoneID, req.Status)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone status updated", nil)
}
