package project

import (
	"context"
	"database/sql"
	"time"

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

// Create creates a new project
func (r *Repository) Create(ctx context.Context, tx *sql.Tx, project *Project) error {
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
func (r *Repository) ListByOrganizationID(ctx context.Context, organizationID uuid.UUID, q pagination.Query) ([]Project, int, error) {
	var projects []Project
	var total int

	countQuery := `
		SELECT count(*)
		FROM projects
		WHERE organization_id = $1
	`

	if err := r.DB.QueryRowContext(ctx, countQuery, organizationID).Scan(&total); err != nil {
		return nil, 0, err
	}

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
		LIMIT $2 OFFSET $3
	`

	rows, err := r.DB.QueryContext(ctx, query, organizationID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	for rows.Next() {
		var p Project

		err := rows.Scan(&p.ID, &p.OrganizationID, &p.CreatedBy, &p.Name, &p.Description, &p.Status, &p.Priority, &p.DueDate, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			return nil, 0, err
		}

		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return projects, total, nil
}

// UpdateDetails updates a project's metadata (name, description, priority,
// due date and/or status). Nil fields are left unchanged.
func (r *Repository) UpdateDetails(ctx context.Context, tx *sql.Tx, projectID uuid.UUID, organizationID uuid.UUID, name *string, description *string, priority *ProjectPriority, status *ProjectStatus, dueDate *time.Time) error {
	query := `
		UPDATE projects
		SET
			name = COALESCE($3, name),
			description = COALESCE($4, description),
			priority = COALESCE($5, priority),
			status = COALESCE($6, status),
			due_date = COALESCE($7, due_date),
			updated_at = NOW()
		WHERE id = $1
		AND organization_id = $2
	`

	result, err := tx.ExecContext(ctx, query, projectID, organizationID, name, description, priority, status, dueDate)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return apperrors.ErrProjectNotFound
	}

	return nil
}

// CreateMilestone creates a new milestone
func (r *Repository) CreateMilestone(ctx context.Context, tx *sql.Tx, milestone *Milestone) error {
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

// ListMilestonesByProject lists all milestones for a project, scoped to an organization
func (r *Repository) ListMilestonesByProjectID(ctx context.Context, db *sql.DB, projectID uuid.UUID, organizationID uuid.UUID, q pagination.Query) ([]Milestone, int, error) {
	var milestones []Milestone
	var total int

	countQuery := `
		SELECT count(*)
		FROM milestones
		WHERE project_id = $1
		AND organization_id = $2
	`

	if err := db.QueryRowContext(ctx, countQuery, projectID, organizationID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			id,
			project_id,
			organization_id,
			title,
			description,
			status,
			due_date,
			position,
			completed_at,
			created_by,
			created_at,
			updated_at
		FROM milestones
		WHERE project_id = $1
		AND organization_id = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := db.QueryContext(ctx, query, projectID, organizationID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	for rows.Next() {
		var m Milestone

		err := rows.Scan(&m.ID, &m.ProjectID, &m.OrganizationID, &m.Title, &m.Description, &m.Status, &m.DueDate, &m.Position, &m.CompletedAt, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, 0, err
		}

		milestones = append(milestones, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return milestones, total, nil
}

// UpdateMilestoneDetails updates a milestone's metadata (title, description,
// due date and/or position). Nil fields are left unchanged.
func (r *Repository) UpdateMilestoneDetails(ctx context.Context, tx *sql.Tx, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID, title *string, description *string, dueDate *time.Time, position *int) error {
	query := `
	UPDATE milestones
	SET
		title = COALESCE($4, title),
		description = COALESCE($5, description),
		due_date = COALESCE($6, due_date),
		position = COALESCE($7, position),
		updated_at = NOW()
	WHERE id = $1
	AND project_id = $2
	AND organization_id = $3
	`

	result, err := tx.ExecContext(ctx, query, milestoneID, projectID, organizationID, title, description, dueDate, position)
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

// --- Tenant Isolation queries ---

// GetByID gets a project by ID
func (r *Repository) GetProjectByIDAndOrganizationID(ctx context.Context, projectID uuid.UUID, organizationID uuid.UUID) (*Project, error) {
	query := `
		SELECT
			id,
			organization_id,
			name,
			description,
			status,
			priority,
			created_by,
			due_date,
			created_at,
			updated_at
		FROM projects
		WHERE id = $1
		AND organization_id = $2
	`

	project := &Project{}

	err := r.DB.QueryRowContext(ctx, query, projectID, organizationID).Scan(&project.ID, &project.OrganizationID, &project.Name, &project.Description, &project.Status, &project.Priority, &project.CreatedBy, &project.DueDate, &project.CreatedAt, &project.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, apperrors.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}

	return project, nil
}

// GetProjectByIDAndOrganizationIDForUpdate gets an organization-scoped
// project and locks it for the duration of the transaction.
func (r *Repository) GetProjectByIDAndOrganizationIDForUpdate(ctx context.Context, tx *sql.Tx, projectID uuid.UUID, organizationID uuid.UUID) (*Project, error) {
	query := `
		SELECT
			id,
			organization_id,
			name,
			description,
			status,
			priority,
			created_by,
			due_date,
			created_at,
			updated_at
		FROM projects
		WHERE id = $1
		AND organization_id = $2
		FOR UPDATE
	`

	project := &Project{}

	err := tx.QueryRowContext(ctx, query, projectID, organizationID).Scan(&project.ID, &project.OrganizationID, &project.Name, &project.Description, &project.Status, &project.Priority, &project.CreatedBy, &project.DueDate, &project.CreatedAt, &project.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, apperrors.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}

	return project, nil
}

// GetMilestoneByIDAndOrganization gets a milestone by ID and organization ID
func (r *Repository) GetMilestoneByIDAndProjectIDAndOrganizationID(ctx context.Context, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID) (*Milestone, error) {
	query := `
		SELECT
			id,
			project_id,
			organization_id,
			title,
			description,
			status,
			due_date,
			position,
			completed_at,
			created_by,
			created_at,
			updated_at
		FROM milestones
		WHERE id = $1
		AND project_id = $2
		AND organization_id = $3
	`

	milestone := &Milestone{}

	err := r.DB.QueryRowContext(ctx, query, milestoneID, projectID, organizationID).Scan(&milestone.ID, &milestone.ProjectID, &milestone.OrganizationID, &milestone.Title, &milestone.Description, &milestone.Status, &milestone.DueDate, &milestone.Position, &milestone.CompletedAt, &milestone.CreatedBy, &milestone.CreatedAt, &milestone.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, apperrors.ErrMilestoneNotFound
	}
	if err != nil {
		return nil, err
	}

	return milestone, nil
}
