package project

import "time"

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	DueDate     string `json:"due_date"`
}

type UpdateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
}

type CreateMilestoneInput struct {
	ProjectID   string
	Title       string
	Description string
	DueDate     *time.Time
}

type UpdateMilestoneStatusInput struct {
	MilestoneID string
	Status      MilestoneStatus
}
