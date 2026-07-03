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
