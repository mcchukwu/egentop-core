package project

import (
	"encoding/json"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/requestctx"
	"github.com/mcchukwu/egentop/internal/response"
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

	// TODO: Validate request

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

	project, err := h.Service.CreateProject(r.Context(), orgID, userID, req)
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
	projectID, ok := requestctx.ProjectID(r.Context())
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
