package project

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

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	projects, meta, err := h.Service.ListByOrganizationID(r.Context(), orgID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "projects fetched", pagination.NewResponse(projects, q, meta.Total))
}

// GetProjectByID gets a project by ID - /projects/{id}
func (h *Handler) GetProjectByID(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	_, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	project, err := h.Service.GetByID(r.Context(), orgID, projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "project fetched", project)
}

// Update updates a project's details (name, description, priority, due date, status)
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateProjectRequest

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

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	project, err := h.Service.Update(r.Context(), userID, orgID, projectID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "project updated", project)
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

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
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
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	_, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	milestones, meta, err := h.Service.ListMilestonesByProjectID(r.Context(), orgID, projectID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestones fetched", pagination.NewResponse(milestones, q, meta.Total))
}

// GetMilestoneByID gets a milestone by ID - /projects/{project_id}/milestones/{milestone_id}
func (h *Handler) GetMilestoneByID(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	_, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	milestone, err := h.Service.GetMilestoneByID(r.Context(), orgID, projectID, milestoneID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone fetched", milestone)
}

// UpdateMilestone updates a milestone's details (title, description, due date, position)
func (h *Handler) UpdateMilestone(w http.ResponseWriter, r *http.Request) {
	var req UpdateMilestoneRequest

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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.UpdateMilestone(r.Context(), orgID, userID, projectID, milestoneID, req)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone updated", milestone)
}
