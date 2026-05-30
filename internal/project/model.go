package project

import "time"

type Status string
type Priority string
type MilestoneStatus string

const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusArchived  Status = "archived"
	StatusCancelled Status = "cancelled"
)

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
)

const (
	MilestoneStatusTodo       MilestoneStatus = "todo"
	MilestoneStatusInProgress MilestoneStatus = "in_progress"
	MilestoneStatusDone       MilestoneStatus = "done"
)

type Project struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id"`
	CreatedBy      string     `json:"created_by"`
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	Status         Status     `json:"status"`
	Priority       Priority   `json:"priority"`
	DueDate        *time.Time `json:"due_date,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

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
