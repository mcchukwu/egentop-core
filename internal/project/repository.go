package project

import (
	"context"
	"database/sql"
	"time"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

type ProjectRepository struct{}

func NewProjectRepository() *ProjectRepository {
	return &ProjectRepository{}
}

// Create creates a new project
func (r *ProjectRepository) Create(ctx context.Context, tx *sql.Tx, project *Project) error {
	query := `
		INSERT INTO projects (name, description, status, priority, due_date, created_by, organization_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`

	err := tx.QueryRowContext(ctx, query, project.Name, project.Description, project.Status, project.Priority, project.DueDate, project.CreatedBy, project.OrganizationID).Scan(&project.ID, &project.CreatedAt, &project.UpdatedAt)
	if err != nil {
		return err
	}

	return nil
}

// ListByOrganization lists all projects for an organization
func (r *ProjectRepository) ListByOrganization(ctx context.Context, db *sql.DB, organizationID string) ([]Project, error) {
	var projects []Project

	query := `
		SELECT
			id,
			organization_id,
			created_by,
			name,
			description,
			status,
			priority,
			due_date,
			created_at,
			updated_at
		FROM projects
		WHERE organization_id = $1
		ORDER BY created_at DESC
	`

	rows, err := db.QueryContext(ctx, query, organizationID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var p Project

		err := rows.Scan(&p.ID, &p.OrganizationID, &p.CreatedBy, &p.Name, &p.Description, &p.Status, &p.Priority, &p.DueDate, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, err
		}

		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return projects, nil
}

// UpdateStatus updates the status of a project
func (r *ProjectRepository) UpdateStatus(ctx context.Context, tx *sql.Tx, projectID string, status Status) error {
	query := `
		UPDATE projects
		SET
			status = $1,
			updated_at = $2
		WHERE id = $3
	`
	_, err := tx.ExecContext(ctx, query, status, time.Now().UTC(), projectID)
	if err != nil {
		return err
	}

	return nil
}

// CreateMilestone creates a new milestone
func (r *ProjectRepository) CreateMilestone(ctx context.Context, tx *sql.Tx, milestone *Milestone) error {
	query := `
	INSERT INTO milestones (
		project_id,
		organization_id,
		title,
		description,
		status,
		due_date,
		created_by,
		created_at,
		updated_at
	)
	VALUES (
		$1,$2,$3,$4,$5,$6,$7,NOW(),NOW()
	)
	RETURNING id, created_at, updated_at
	`

	err := tx.QueryRowContext(ctx, query, milestone.ProjectID, milestone.OrganizationID, milestone.Title, milestone.Description, milestone.Status, milestone.DueDate, milestone.CreatedBy).Scan(&milestone.ID, &milestone.CreatedAt, &milestone.UpdatedAt)
	if err != nil {
		return err
	}

	return nil
}

// UpdateMilestoneStatus updates the status of a milestone
func (r *ProjectRepository) UpdateMilestoneStatus(ctx context.Context, tx *sql.Tx, milestoneID string, status MilestoneStatus) error {
	query := `
	UPDATE milestones
	SET
		status = $1,
		updated_at = NOW()
	WHERE id = $2
	`

	result, err := tx.ExecContext(ctx, query, status, milestoneID)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return apperrors.ErrMilestoneNotFound
	}

	return nil
}
