package project

import "time"

type Status string

const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusArchived  Status = "archived"
	StatusCancelled Status = "cancelled"
)

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
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
