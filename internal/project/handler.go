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

	role, _ := requestctx.Role(r.Context())

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	projects, meta, err := h.Service.ListByOrganizationID(r.Context(), orgID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Client actors never see the agency-facing revision limit field. The
	// project.list permission is staff-only today, but the payload split is
	// kept at the handler level so a client-role response can never carry it.
	if role == "client" {
		response.Success(w, http.StatusOK, "projects fetched", pagination.NewResponse(projects, q, meta.Total))
		return
	}

	details := make([]ProjectDetail, 0, len(projects))
	for _, p := range projects {
		details = append(details, newProjectDetail(&p))
	}

	response.Success(w, http.StatusOK, "projects fetched", pagination.NewResponse(details, q, meta.Total))
}

// GetProjectByID gets a project by ID - /projects/{id}
func (h *Handler) GetProjectByID(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(r.PathValue("projectID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	role, _ := requestctx.Role(r.Context())

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	project, err := h.Service.ViewProject(r.Context(), userID, role, orgID, projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Client actors never see the agency-facing revision limit field.
	if role == "client" {
		response.Success(w, http.StatusOK, "project fetched", project)
		return
	}

	response.Success(w, http.StatusOK, "project fetched", newProjectDetail(project))
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

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	role, _ := requestctx.Role(r.Context())

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	milestones, meta, err := h.Service.ListMilestonesByProjectID(r.Context(), userID, role, orgID, projectID, q)
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

	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	role, _ := requestctx.Role(r.Context())

	orgID, ok := requestctx.OrganizationID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	milestone, err := h.Service.GetMilestoneDetail(r.Context(), userID, role, orgID, projectID, milestoneID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	// Client actors never see the agency-facing revision limit fields.
	if role == "client" {
		response.Success(w, http.StatusOK, "milestone fetched", newClientMilestoneDetail(milestone))
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

// AssignClient assigns, reassigns, or unassigns a project's client -
// PUT /projects/{project_id}/client
func (h *Handler) AssignClient(w http.ResponseWriter, r *http.Request) {
	var req AssignClientRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
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

	project, err := h.Service.AssignClient(r.Context(), userID, orgID, projectID, req.ClientID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "client assigned", project)
}

// SubmitMilestone submits a milestone for client approval -
// POST /projects/{project_id}/milestones/{milestone_id}/submit
func (h *Handler) SubmitMilestone(w http.ResponseWriter, r *http.Request) {
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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.SubmitMilestone(r.Context(), userID, orgID, projectID, milestoneID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone submitted for approval", milestone)
}

// ApproveMilestone approves a submitted milestone (client sign-off) -
// POST /projects/{project_id}/milestones/{milestone_id}/approve
func (h *Handler) ApproveMilestone(w http.ResponseWriter, r *http.Request) {
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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.ApproveMilestone(r.Context(), userID, orgID, projectID, milestoneID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone approved", milestone)
}

// RequestMilestoneChanges requests changes on a submitted milestone -
// POST /projects/{project_id}/milestones/{milestone_id}/changes-requested
func (h *Handler) RequestMilestoneChanges(w http.ResponseWriter, r *http.Request) {
	var req RequestChangesRequest

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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.RequestMilestoneChanges(r.Context(), userID, orgID, projectID, milestoneID, req.Notes)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "changes requested", milestone)
}

// UpdateMilestoneStatus applies a generic staff status transition -
// PATCH /projects/{project_id}/milestones/{milestone_id}/status
func (h *Handler) UpdateMilestoneStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateMilestoneStatusRequest

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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.UpdateMilestoneStatus(r.Context(), userID, orgID, projectID, milestoneID, req.Status)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone status updated", milestone)
}

// CreateDeliverable adds a link-based deliverable -
// POST /projects/{project_id}/milestones/{milestone_id}/deliverables
func (h *Handler) CreateDeliverable(w http.ResponseWriter, r *http.Request) {
	var req CreateDeliverableRequest

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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	deliverable, err := h.Service.CreateDeliverable(r.Context(), userID, orgID, projectID, milestoneID, req.URL, req.Title, req.Description)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusCreated, "deliverable submitted", deliverable)
}

// DeleteDeliverable removes a deliverable -
// DELETE /projects/{project_id}/milestones/{milestone_id}/deliverables/{deliverable_id}
func (h *Handler) DeleteDeliverable(w http.ResponseWriter, r *http.Request) {
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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	deliverableID, err := uuid.Parse(r.PathValue("deliverableID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	if err := h.Service.DeleteDeliverable(r.Context(), userID, orgID, projectID, milestoneID, deliverableID); err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "deliverable removed", nil)
}

// UpdateMilestonePaymentStatus updates a milestone's payment status -
// PATCH /projects/{project_id}/milestones/{milestone_id}/payment-status
func (h *Handler) UpdateMilestonePaymentStatus(w http.ResponseWriter, r *http.Request) {
	var req UpdateMilestonePaymentStatusRequest

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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.UpdateMilestonePaymentStatus(r.Context(), userID, orgID, projectID, milestoneID, req.Status)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone payment status updated", milestone)
}

// UpdateProjectRevisionLimit sets or clears the project-level revision limit -
// PATCH /projects/{project_id}/revision-limit
func (h *Handler) UpdateProjectRevisionLimit(w http.ResponseWriter, r *http.Request) {
	var req UpdateRevisionLimitRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	if req.RevisionLimit != nil && *req.RevisionLimit < 1 {
		response.ValidationError(w, map[string]string{"revision_limit": "must be null or at least 1"})
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

	project, err := h.Service.SetProjectRevisionLimit(r.Context(), userID, orgID, projectID, req.RevisionLimit)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "project revision limit updated", project)
}

// UpdateMilestoneRevisionLimit sets or clears the per-milestone revision-limit
// override - PATCH /projects/{project_id}/milestones/{milestone_id}/revision-limit
func (h *Handler) UpdateMilestoneRevisionLimit(w http.ResponseWriter, r *http.Request) {
	var req UpdateRevisionLimitRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	if req.RevisionLimit != nil && *req.RevisionLimit < 1 {
		response.ValidationError(w, map[string]string{"revision_limit": "must be null or at least 1"})
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

	milestoneID, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		response.HandleError(w, apperrors.ErrInvalidRequestBody)
		return
	}

	milestone, err := h.Service.SetMilestoneRevisionLimit(r.Context(), userID, orgID, projectID, milestoneID, req.RevisionLimit)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "milestone revision limit updated", milestone)
}

// GetApprovalView returns the shared client-facing deep link payload -
// GET /projects/{project_id}/approval
func (h *Handler) GetApprovalView(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	role, _ := requestctx.Role(r.Context())

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

	view, err := h.Service.GetApprovalView(r.Context(), userID, role, orgID, projectID)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "approval view fetched", view)
}

// ListProjectActivities lists the project-scoped activity feed -
// GET /projects/{project_id}/activities
func (h *Handler) ListProjectActivities(w http.ResponseWriter, r *http.Request) {
	userID, ok := requestctx.UserID(r.Context())
	if !ok {
		response.HandleError(w, apperrors.ErrUnauthorized)
		return
	}

	role, _ := requestctx.Role(r.Context())

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

	q := pagination.Parse(r.URL.Query().Get("page"), r.URL.Query().Get("limit"))

	activities, meta, err := h.Service.ListProjectActivities(r.Context(), userID, role, orgID, projectID, q)
	if err != nil {
		response.HandleError(w, err)
		return
	}

	response.Success(w, http.StatusOK, "activities fetched", pagination.NewResponse(activities, q, meta.Total))
}
