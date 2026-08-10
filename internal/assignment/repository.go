package assignment

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/pkg/pagination"
)

type Repository struct {
	DB *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		DB: db,
	}
}

// Create creates a new assignment
func (r *Repository) Create(ctx context.Context, tx *sql.Tx, assignment *Assignment) error {
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

// GetByID returns an assignment by ID scoped to an organization
func (r *Repository) GetByIDAndOrganizationID(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, assignmentID uuid.UUID, assignment *Assignment) error {
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
		AND organization_id = $2
	`

	err := tx.QueryRowContext(ctx, query, assignmentID, orgID).Scan(&assignment.ID, &assignment.OrganizationID, &assignment.ProjectID, &assignment.MilestoneID, &assignment.AssignedTo, &assignment.AssignedBy, &assignment.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrAssignmentNotFound
		}

		return err
	}

	return nil
}

// ListByProjectID returns all assignments for a project within an organization.
func (r *Repository) ListByProjectID(ctx context.Context, orgID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]Assignment, int, error) {
	var assignments []Assignment
	var total int

	countQuery := `
		SELECT count(*)
		FROM assignments
		WHERE organization_id = $1
		AND project_id = $2
	`

	if err := r.DB.QueryRowContext(ctx, countQuery, orgID, projectID).Scan(&total); err != nil {
		return nil, 0, err
	}

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
		WHERE organization_id = $1
		AND project_id = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := r.DB.QueryContext(ctx, query, orgID, projectID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	for rows.Next() {
		var a Assignment

		err := rows.Scan(&a.ID, &a.OrganizationID, &a.ProjectID, &a.MilestoneID, &a.AssignedTo, &a.AssignedBy, &a.CreatedAt)
		if err != nil {
			return nil, 0, err
		}

		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return assignments, total, nil
}

// UpdateAssignedTo reassigns an assignment to a new user, scoped to an organization.
func (r *Repository) UpdateAssignedTo(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, assignmentID uuid.UUID, assignedTo uuid.UUID) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE assignments
		SET assigned_to = $1
		WHERE id = $2
		AND organization_id = $3
	`, assignedTo, assignmentID, orgID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return apperrors.ErrAssignmentNotFound
	}

	return nil
}

// Delete removes an assignment, scoped to an organization.
func (r *Repository) Delete(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, assignmentID uuid.UUID) error {
	result, err := tx.ExecContext(ctx, `
		DELETE FROM assignments
		WHERE id = $1
		AND organization_id = $2
	`, assignmentID, orgID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return apperrors.ErrAssignmentNotFound
	}

	return nil
}
