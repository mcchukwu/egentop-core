package project

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
	"github.com/mcchukwu/egentop/internal/validation"
)

type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{Service: service}
}

// Create creates a new project - /projects
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
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

	project, err := h.Service.Create(r.Context(), userID, organizationID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "project created", project)
}

// ListByOrganizationID lists all projects for an organization - /projects
func (h *Handler) ListProjectsByOrganizationID(w http.ResponseWriter, r *http.Request) {
	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	projects, err := h.Service.ListByOrganizationID(r.Context(), orgID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "projects fetched", projects)
}

// GetProjectByID gets a project by ID - /projects/{id}
// GetProjectByID gets a project by ID - /projects/{id}
// TODO: pagination
func (h *Handler) GetProjectByID(w http.ResponseWriter, r *http.Request) {
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

	project, err := h.Service.GetByID(r.Context(), projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "project fetched", project)
}

// UpdateProjectStatus updates the status of a project
func (h *Handler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateProjectStatusInput

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

	projectID := r.PathValue("project_id")
	if projectID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	err := h.Service.UpdateStatus(r.Context(), orgID, userID, projectID, req.Status)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "project status updated", nil)
}

// CreateMilestone creates a new milestone - /projects/{project_id}/milestones
func (h *Handler) CreateMilestone(w http.ResponseWriter, r *http.Request) {
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

	projectID := r.PathValue("project_id")
	if projectID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.CreateMilestone(r.Context(), orgID, projectID, userID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "milestone created", milestone)
}

// ListMilestonesByProjectID lists all milestones for a project - /projects/{project_id}/milestones
func (h *Handler) ListMilestonesByProjectID(w http.ResponseWriter, r *http.Request) {
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

	milestones, err := h.Service.ListMilestonesByProjectID(r.Context(), projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestones fetched", milestones)
	// TODO: pagination
}

// GetMilestoneByID gets a milestone by ID - /projects/{project_id}/milestones/{milestone_id}
func (h *Handler) GetMilestoneByID(w http.ResponseWriter, r *http.Request) {
	milestoneID := r.PathValue("milestone_id")
	if milestoneID == "" {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	_, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	milestone, err := h.Service.GetMilestoneByID(r.Context(), milestoneID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone fetched", milestone)
}

// UpdateMilestoneStatus updates the status of a milestone
func (h *Handler) UpdateMilestoneStatus(w http.ResponseWriter, r *http.Request) {
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
