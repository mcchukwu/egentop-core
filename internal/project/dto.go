package project

import "time"

type CreateProjectRequest struct {
	Name        string          `json:"name" validate:"required,min=3,max=120"`
	Description string          `json:"description" validate:"max=2000"`
	Priority    ProjectPriority `json:"priority"`
	DueDate     *time.Time      `json:"due_date"`
}
type UpdateProjectRequest struct {
	Name        string          `json:"name" validate:"required,min=3,max=120"`
	Description string          `json:"description" validate:"max=2000"`
	Priority    ProjectPriority `json:"priority"`
	Status      ProjectStatus   `json:"status"`
}

type CreateMilestoneInput struct {
	ProjectID   string     `json:"project_id" validate:"required"`
	Title       string     `json:"title" validate:"required,min=3,max=120"`
	Description string     `json:"description" validate:"max=2000"`
	DueDate     *time.Time `json:"due_date"`
}
type UpdateMilestoneStatusInput struct {
	Status MilestoneStatus `json:"status" validate:"required"`
}
