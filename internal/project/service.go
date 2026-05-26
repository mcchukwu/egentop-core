package project

import (
	"context"
	"time"
)

type ProjectService struct {
	Repo *ProjectRepository
}

func NewProjectService(repo *ProjectRepository) *ProjectService {
	return &ProjectService{Repo: repo}
}

func (s *ProjectService) CreateProject(ctx context.Context, organizationID string, createdBy string, req CreateProjectRequest) (*Project, error) {

	// TODO: Validate request

	project := &Project{
		OrganizationID: organizationID,
		CreatedBy:      createdBy,
		Name:           req.Name,
		Status:         StatusDraft,
		Priority:       PriorityMedium,
	}

	if req.Description != "" {
		project.Description = &req.Description
	}

	if req.Priority != "" {
		project.Priority = Priority(req.Priority)
	}

	if req.DueDate != "" {
		parsed, err := time.Parse(time.RFC3339, req.DueDate)
		if err == nil {
			project.DueDate = &parsed
		}
	}

	if err := s.Repo.Create(ctx, project); err != nil {
		return nil, err
	}

	return project, nil
}

func (s *ProjectService) ListProjects(ctx context.Context, organizationID string) ([]Project, error) {
	return s.Repo.ListByOrganization(ctx, organizationID)
}
