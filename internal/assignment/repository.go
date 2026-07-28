package assignment

import (
	"context"
	"database/sql"
)

type AssignmentRepository struct {
	DB *sql.DB
}

func NewAssignmentRepository(db *sql.DB) *AssignmentRepository {
	return &AssignmentRepository{
		DB: db,
	}
}

// Create creates a new assignment
func (r *AssignmentRepository) Create(ctx context.Context, tx *sql.Tx, assignment *Assignment) error {
	query := `
		INSERT INTO assignments (
			organization_id,
			project_id,
			milestone_id,
			assigned_to,
			assigned_by,
			created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at
	`

	err := tx.QueryRowContext(ctx, query, assignment.OrganizationID, assignment.ProjectID, assignment.MilestoneID, assignment.AssignedTo, assignment.AssignedBy, assignment.CreatedAt).Scan(&assignment.ID, &assignment.CreatedAt)
	if err != nil {
		return err
	}

	return nil
}

// GetByID returns an assignment by ID
func (r *AssignmentRepository) GetByID(ctx context.Context, tx *sql.Tx, assignmentID string, assignment *Assignment) error {
	query := `
		SELECT
			id,
			organization_id,
			project_id,
			milestone_id,
			assigned_to,
			assigned_by,
			created_at
		FROM assignments
		WHERE id = $1
	`

	err := tx.QueryRowContext(ctx, query, assignmentID).Scan(&assignment.ID, &assignment.OrganizationID, &assignment.ProjectID, &assignment.MilestoneID, &assignment.AssignedTo, &assignment.AssignedBy, &assignment.CreatedAt)
	if err != nil {
		return err
	}

	return nil
}
