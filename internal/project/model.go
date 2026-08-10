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
)

type Milestone struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	ProjectID      uuid.UUID       `json:"project_id"`
	CreatedBy      uuid.UUID       `json:"created_by"`
	Title          string          `json:"title"`
	Description    *string         `json:"description,omitempty"`
	Status         MilestoneStatus `json:"status"`
	DueDate        *time.Time      `json:"due_date,omitempty"`
	Position       int             `json:"position"`
	CompletedAt    *time.Time      `json:"completed_at,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}
