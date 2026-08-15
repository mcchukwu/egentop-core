package project

import (
	"time"

	"github.com/google/uuid"
)

type CreateProjectRequest struct {
	Name        string          `json:"name" validate:"required,min=3,max=120"`
	Description string          `json:"description" validate:"omitempty,max=2000"`
	Priority    ProjectPriority `json:"priority" validate:"omitempty"`
	DueDate     *time.Time      `json:"due_date" validate:"omitempty"`
}
type UpdateProjectRequest struct {
	Name        string          `json:"name" validate:"omitempty,min=3,max=120"`
	Description string          `json:"description" validate:"omitempty,max=2000"`
	Priority    ProjectPriority `json:"priority" validate:"omitempty"`
	Status      ProjectStatus   `json:"status" validate:"omitempty"`
	DueDate     *time.Time      `json:"due_date" validate:"omitempty"`
}

type CreateMilestoneInput struct {
	Title       string     `json:"title" validate:"required,min=3,max=120"`
	Description string     `json:"description" validate:"max=2000"`
	DueDate     *time.Time `json:"due_date"`
}
type UpdateMilestoneRequest struct {
	Title       string     `json:"title" validate:"omitempty,min=3,max=120"`
	Description string     `json:"description" validate:"omitempty,max=2000"`
	DueDate     *time.Time `json:"due_date" validate:"omitempty"`
	Position    int        `json:"position" validate:"omitempty"`
}

// AssignClientRequest is the body of PUT /projects/{projectID}/client.
// A null client_id unassigns the current client.
type AssignClientRequest struct {
	ClientID *uuid.UUID `json:"client_id"`
}

// UpdateMilestoneStatusRequest is the body of the generic staff status PATCH.
// Only the staff-reachable statuses are accepted: the action-only statuses
// (awaiting_approval, approved, changes_requested) are reached exclusively
// through submit/approve/changes-requested and rejected here with a field
// error.
type UpdateMilestoneStatusRequest struct {
	Status MilestoneStatus `json:"status" validate:"required,oneof=pending in_progress completed blocked cancelled"`
}

// RequestChangesRequest is the body of POST .../milestones/{id}/changes-requested.
type RequestChangesRequest struct {
	Notes string `json:"notes" validate:"required,min=3,max=2000"`
}

// CreateDeliverableRequest is the body of POST .../milestones/{id}/deliverables.
type CreateDeliverableRequest struct {
	URL         string  `json:"url" validate:"required,url,max=2000"`
	Title       *string `json:"title,omitempty" validate:"omitempty,max=200"`
	Description *string `json:"description,omitempty" validate:"omitempty,max=2000"`
}

// UpdateMilestonePaymentStatusRequest is the body of the payment-status PATCH.
type UpdateMilestonePaymentStatusRequest struct {
	Status MilestonePaymentStatus `json:"status" validate:"required,oneof=unpaid partial paid"`
}

// UpdateRevisionLimitRequest is the body of the agency-only revision-limit
// PATCHes (project default + per-milestone override). A nil value (absent or
// JSON null) clears the limit / override. Values below 1 are rejected with a
// field validation error before reaching the DB CHECK constraint.
type UpdateRevisionLimitRequest struct {
	RevisionLimit *int `json:"revision_limit"`
}

// ClientMilestoneDetail is the milestone detail payload for client-role
// actors. revision_count + payment_status + deliverables are visible (the
// client approval surface), but the agency-facing revision limit and
// limit_reached fields are absent.
type ClientMilestoneDetail struct {
	ID             uuid.UUID              `json:"id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	ProjectID      uuid.UUID              `json:"project_id"`
	Title          string                 `json:"title"`
	Description    *string                `json:"description,omitempty"`
	Status         MilestoneStatus        `json:"status"`
	DueDate        *time.Time             `json:"due_date,omitempty"`
	Position       int                    `json:"position"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	RevisionCount  int                    `json:"revision_count"`
	PaymentStatus  MilestonePaymentStatus `json:"payment_status"`
	Deliverables   []Deliverable          `json:"deliverables,omitempty"`
}

// newClientMilestoneDetail maps a milestone to the client-scoped detail view.
func newClientMilestoneDetail(m *Milestone) ClientMilestoneDetail {
	return ClientMilestoneDetail{
		ID:             m.ID,
		OrganizationID: m.OrganizationID,
		ProjectID:      m.ProjectID,
		Title:          m.Title,
		Description:    m.Description,
		Status:         m.Status,
		DueDate:        m.DueDate,
		Position:       m.Position,
		CompletedAt:    m.CompletedAt,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
		RevisionCount:  m.RevisionCount,
		PaymentStatus:  m.PaymentStatus,
		Deliverables:   m.Deliverables,
	}
}

// ProjectDetail is the project payload for staff-role actors on the list and
// detail reads. It mirrors the Project model and additionally carries the
// agency-facing revision_limit (NULL = unlimited). Client-role actors receive
// the plain Project payload, which never serializes the field; the approval
// deep link keeps it absent as well.
type ProjectDetail struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	CreatedBy      uuid.UUID       `json:"created_by"`
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	Status         ProjectStatus   `json:"status"`
	Priority       ProjectPriority `json:"priority"`
	DueDate        *time.Time      `json:"due_date,omitempty"`
	ClientID       *uuid.UUID      `json:"client_id,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	RevisionLimit  *int            `json:"revision_limit,omitempty"`
}

// newProjectDetail maps a project to the staff-scoped payload.
func newProjectDetail(p *Project) ProjectDetail {
	return ProjectDetail{
		ID:             p.ID,
		OrganizationID: p.OrganizationID,
		CreatedBy:      p.CreatedBy,
		Name:           p.Name,
		Description:    p.Description,
		Status:         p.Status,
		Priority:       p.Priority,
		DueDate:        p.DueDate,
		ClientID:       p.ClientID,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
		RevisionLimit:  p.RevisionLimit,
	}
}

// ApprovalView is the shared deep-link payload (the WhatsApp landing page).
// limit_reached / revision_limit are deliberately absent.
type ApprovalView struct {
	Project    Project             `json:"project"`
	Milestones []ApprovalMilestone `json:"milestones"`
}

type ApprovalMilestone struct {
	ID            uuid.UUID              `json:"id"`
	ProjectID     uuid.UUID              `json:"project_id"`
	Title         string                 `json:"title"`
	Description   *string                `json:"description,omitempty"`
	Status        MilestoneStatus        `json:"status"`
	DueDate       *time.Time             `json:"due_date,omitempty"`
	Position      int                    `json:"position"`
	RevisionCount int                    `json:"revision_count"`
	PaymentStatus MilestonePaymentStatus `json:"payment_status"`
	Deliverables  []Deliverable          `json:"deliverables"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}
