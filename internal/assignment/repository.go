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

// Create creates a new assignment. created_at is intentionally not part of
// the INSERT: passing the struct's zero time.Time would override the column's
// DEFAULT NOW() and store (and return) year-1 timestamps. The DB default
// applies and RETURNING created_at returns the real value.
func (r *Repository) Create(ctx context.Context, tx *sql.Tx, assignment *Assignment) error {
	query := `
		INSERT INTO assignments (
			organization_id,
			project_id,
			milestone_id,
			assigned_to,
			assigned_by
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`

	err := tx.QueryRowContext(ctx, query, assignment.OrganizationID, assignment.ProjectID, assignment.MilestoneID, assignment.AssignedTo, assignment.AssignedBy).Scan(&assignment.ID, &assignment.CreatedAt)
	if err != nil {
		return err
	}

	return nil
}

// GetByID returns an assignment by ID scoped to an organization. The
// assignee and assigning-user display names are resolved by users joins.
func (r *Repository) GetByIDAndProjectIDAndOrganizationID(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, projectID uuid.UUID, assignmentID uuid.UUID, assignment *Assignment) error {
	query := `
		SELECT
			a.id,
			a.organization_id,
			a.project_id,
			a.milestone_id,
			a.assigned_to,
			a.assigned_by,
			a.created_at,
			u.first_name || ' ' || u.last_name AS assignee_name,
			u2.first_name || ' ' || u2.last_name AS assigned_by_name
		FROM assignments a
		LEFT JOIN users u ON u.id = a.assigned_to
		LEFT JOIN users u2 ON u2.id = a.assigned_by
		WHERE a.id = $1
		AND a.organization_id = $2
		AND a.project_id = $3
	`

	var assigneeName, assignedByName sql.NullString

	err := tx.QueryRowContext(ctx, query, assignmentID, orgID, projectID).Scan(&assignment.ID, &assignment.OrganizationID, &assignment.ProjectID, &assignment.MilestoneID, &assignment.AssignedTo, &assignment.AssignedBy, &assignment.CreatedAt, &assigneeName, &assignedByName)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrAssignmentNotFound
		}

		return err
	}

	if assigneeName.Valid {
		assignment.AssigneeName = &assigneeName.String
	}
	if assignedByName.Valid {
		assignment.AssignedByName = &assignedByName.String
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
			a.id,
			a.organization_id,
			a.project_id,
			a.milestone_id,
			a.assigned_to,
			a.assigned_by,
			a.created_at,
			u.first_name || ' ' || u.last_name AS assignee_name,
			u2.first_name || ' ' || u2.last_name AS assigned_by_name
		FROM assignments a
		LEFT JOIN users u ON u.id = a.assigned_to
		LEFT JOIN users u2 ON u2.id = a.assigned_by
		WHERE a.organization_id = $1
		AND a.project_id = $2
		ORDER BY a.created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := r.DB.QueryContext(ctx, query, orgID, projectID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	for rows.Next() {
		var a Assignment
		var assigneeName, assignedByName sql.NullString

		err := rows.Scan(&a.ID, &a.OrganizationID, &a.ProjectID, &a.MilestoneID, &a.AssignedTo, &a.AssignedBy, &a.CreatedAt, &assigneeName, &assignedByName)
		if err != nil {
			return nil, 0, err
		}

		if assigneeName.Valid {
			a.AssigneeName = &assigneeName.String
		}
		if assignedByName.Valid {
			a.AssignedByName = &assignedByName.String
		}

		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return assignments, total, nil
}

// UpdateAssignedTo reassigns an assignment to a new user, scoped to an organization.
func (r *Repository) UpdateAssignedTo(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, projectID uuid.UUID, assignmentID uuid.UUID, assignedTo uuid.UUID) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE assignments
		SET assigned_to = $1
		WHERE id = $2
		AND organization_id = $3
		AND project_id = $4
	`, assignedTo, assignmentID, orgID, projectID)
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
func (r *Repository) Delete(ctx context.Context, tx *sql.Tx, orgID uuid.UUID, projectID uuid.UUID, assignmentID uuid.UUID) error {
	result, err := tx.ExecContext(ctx, `
		DELETE FROM assignments
		WHERE id = $1
		AND organization_id = $2
		AND project_id = $3
	`, assignmentID, orgID, projectID)
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

// EnsureProjectInOrganization verifies the URL project belongs to the active
// organization before an assignment is created.
func (r *Repository) EnsureProjectInOrganization(ctx context.Context, tx *sql.Tx, orgID, projectID uuid.UUID) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM projects
			WHERE id = $1 AND organization_id = $2
		)
	`, projectID, orgID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return apperrors.ErrProjectNotFound
	}
	return nil
}

// EnsureMilestoneInProject verifies that the assignment target milestone is
// owned by both the requested project and organization.
func (r *Repository) EnsureMilestoneInProject(ctx context.Context, tx *sql.Tx, orgID, projectID, milestoneID uuid.UUID) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM milestones
			WHERE id = $1 AND project_id = $2 AND organization_id = $3
		)
	`, milestoneID, projectID, orgID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return apperrors.ErrMilestoneNotFound
	}
	return nil
}

// EnsureActiveMember verifies that the assignee has an active membership in
// the organization with a STAFF role (client-role memberships are excluded:
// assignments are a staff concept). The row is locked for the duration of the
// create transaction so a concurrent membership change cannot pass this check
// after the assignment is inserted.
func (r *Repository) EnsureActiveMember(ctx context.Context, tx *sql.Tx, orgID, userID uuid.UUID) error {
	var found uuid.UUID
	err := tx.QueryRowContext(ctx, `
		SELECT m.id
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1 AND m.user_id = $2 AND m.status = 'active'
		AND r.name <> 'client'
		FOR UPDATE OF m
	`, orgID, userID).Scan(&found)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrMembershipNotFound
		}
		return err
	}

	return nil
}
