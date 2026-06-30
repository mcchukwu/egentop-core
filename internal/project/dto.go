package project

import "time"

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
}
type UpdateProjectStatusInput struct {
	Status ProjectStatus `json:"status" validate:"required"`
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
type UpdateMilestoneStatusInput struct {
	Status MilestoneStatus `json:"status" validate:"required"`
}
