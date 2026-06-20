package project

import "time"

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
	ID             string          `json:"id"`
	OrganizationID string          `json:"organization_id"`
	CreatedBy      string          `json:"created_by"`
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
	ID             string
	OrganizationID string
	ProjectID      string
	Title          string
	Description    string
	Status         MilestoneStatus
	DueDate        *time.Time
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
