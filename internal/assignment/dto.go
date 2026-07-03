package assignment

type CreateAssignmentRequest struct {
	MilestoneID string `json:"milestone_id" validate:"required,uuid"`
	AssignedTo  string `json:"assigned_to" validate:"required,uuid"`
}

type UpdateAssignmentRequest struct {
	AssignedTo string `json:"assigned_to" validate:"required,uuid"`
}
