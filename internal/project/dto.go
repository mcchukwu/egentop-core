package project

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
