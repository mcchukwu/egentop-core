package project

import (
	"time"

	"github.com/google/uuid"
)

type ProjectStatus string

const (
	ProjectStatusDraft     ProjectStatus = "draft"
	ProjectStatusActive    ProjectStatus = "active"
	ProjectStatusCompleted ProjectStatus = "completed"
	ProjectStatusArchived  ProjectStatus = "archived"
	ProjectStatusCancelled ProjectStatus = "cancelled"
)

type ProjectPriority string

const (
	ProjectPriorityLow    ProjectPriority = "low"
	ProjectPriorityMedium ProjectPriority = "medium"
	ProjectPriorityHigh   ProjectPriority = "high"
)

type Project struct {
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
}

type MilestoneStatus string

const (
	MilestoneStatusPending          MilestoneStatus = "pending"
	MilestoneStatusInProgress       MilestoneStatus = "in_progress"
	MilestoneStatusAwaitingApproval MilestoneStatus = "awaiting_approval"
	MilestoneStatusCompleted        MilestoneStatus = "completed"
	MilestoneStatusBlocked          MilestoneStatus = "blocked"
	MilestoneStatusCancelled        MilestoneStatus = "cancelled"
	MilestoneStatusApproved         MilestoneStatus = "approved"
	MilestoneStatusChangesRequested MilestoneStatus = "changes_requested"
)

type MilestonePaymentStatus string

const (
	MilestonePaymentStatusUnpaid  MilestonePaymentStatus = "unpaid"
	MilestonePaymentStatusPartial MilestonePaymentStatus = "partial"
	MilestonePaymentStatusPaid    MilestonePaymentStatus = "paid"
)

type Milestone struct {
	ID             uuid.UUID              `json:"id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	ProjectID      uuid.UUID              `json:"project_id"`
	CreatedBy      uuid.UUID              `json:"created_by"`
	Title          string                 `json:"title"`
	Description    *string                `json:"description,omitempty"`
	Status         MilestoneStatus        `json:"status"`
	DueDate        *time.Time             `json:"due_date,omitempty"`
	Position       int                    `json:"position"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	RevisionCount  int                    `json:"revision_count"`
	RevisionLimit  *int                   `json:"revision_limit,omitempty"`
	LimitReached   bool                   `json:"limit_reached"`
	PaymentStatus  MilestonePaymentStatus `json:"payment_status"`
	Deliverables   []Deliverable          `json:"deliverables,omitempty"`
}

// Deliverable is a link-based deliverable on a milestone. Link-based only for
// the MVP: no object storage. Delete + re-add is the edit mechanism.
type Deliverable struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	MilestoneID    uuid.UUID `json:"milestone_id"`
	URL            string    `json:"url"`
	Title          *string   `json:"title,omitempty"`
	Description    *string   `json:"description,omitempty"`
	SubmittedBy    uuid.UUID `json:"submitted_by"`
	SubmittedAt    time.Time `json:"submitted_at"`
}
