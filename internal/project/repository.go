package project

import (
	"context"
	"database/sql"
	"errors"
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
			client_id,
			created_at,
			updated_at,
			revision_limit
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

		err := rows.Scan(&p.ID, &p.OrganizationID, &p.CreatedBy, &p.Name, &p.Description, &p.Status, &p.Priority, &p.DueDate, &p.ClientID, &p.CreatedAt, &p.UpdatedAt, &p.RevisionLimit)
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
func (r *Repository) ListMilestonesByProjectID(ctx context.Context, projectID uuid.UUID, organizationID uuid.UUID, q pagination.Query) ([]Milestone, int, error) {
	var milestones []Milestone
	var total int

	countQuery := `
		SELECT count(*)
		FROM milestones
		WHERE project_id = $1
		AND organization_id = $2
	`

	if err := r.DB.QueryRowContext(ctx, countQuery, projectID, organizationID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			m.id,
			m.project_id,
			m.organization_id,
			m.title,
			m.description,
			m.status,
			m.due_date,
			m.position,
			m.completed_at,
			m.created_by,
			m.created_at,
			m.updated_at,
			m.revision_count,
			COALESCE(m.revision_limit, p.revision_limit) AS effective_revision_limit,
			m.payment_status
		FROM milestones m
		JOIN projects p ON p.id = m.project_id
		WHERE m.project_id = $1
		AND m.organization_id = $2
		ORDER BY m.created_at DESC
		LIMIT $3 OFFSET $4
	`

	rows, err := r.DB.QueryContext(ctx, query, projectID, organizationID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	for rows.Next() {
		var m Milestone

		err := rows.Scan(&m.ID, &m.ProjectID, &m.OrganizationID, &m.Title, &m.Description, &m.Status, &m.DueDate, &m.Position, &m.CompletedAt, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.RevisionCount, &m.RevisionLimit, &m.PaymentStatus)
		if err != nil {
			return nil, 0, err
		}

		m.LimitReached = revisionLimitReached(m.RevisionCount, m.RevisionLimit)

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

// GetProjectByIDAndOrganizationID gets an organization-scoped project.
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
			client_id,
			created_at,
			updated_at,
			revision_limit
		FROM projects
		WHERE id = $1
		AND organization_id = $2
	`

	project := &Project{}

	err := r.DB.QueryRowContext(ctx, query, projectID, organizationID).Scan(&project.ID, &project.OrganizationID, &project.Name, &project.Description, &project.Status, &project.Priority, &project.CreatedBy, &project.DueDate, &project.ClientID, &project.CreatedAt, &project.UpdatedAt, &project.RevisionLimit)
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
			client_id,
			created_at,
			updated_at,
			revision_limit
		FROM projects
		WHERE id = $1
		AND organization_id = $2
		FOR UPDATE
	`

	project := &Project{}

	err := tx.QueryRowContext(ctx, query, projectID, organizationID).Scan(&project.ID, &project.OrganizationID, &project.Name, &project.Description, &project.Status, &project.Priority, &project.CreatedBy, &project.DueDate, &project.ClientID, &project.CreatedAt, &project.UpdatedAt, &project.RevisionLimit)
	if err == sql.ErrNoRows {
		return nil, apperrors.ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}

	return project, nil
}

const milestoneColumns = `
	m.id,
	m.project_id,
	m.organization_id,
	m.title,
	m.description,
	m.status,
	m.due_date,
	m.position,
	m.completed_at,
	m.created_by,
	m.created_at,
	m.updated_at,
	m.revision_count,
	COALESCE(m.revision_limit, p.revision_limit) AS effective_revision_limit,
	m.payment_status
`

const milestoneSelectFrom = `
	FROM milestones m
	JOIN projects p ON p.id = m.project_id
`

func scanMilestoneRow(scan func(dest ...any) error) (*Milestone, error) {
	var m Milestone

	err := scan(&m.ID, &m.ProjectID, &m.OrganizationID, &m.Title, &m.Description, &m.Status, &m.DueDate, &m.Position, &m.CompletedAt, &m.CreatedBy, &m.CreatedAt, &m.UpdatedAt, &m.RevisionCount, &m.RevisionLimit, &m.PaymentStatus)
	if err != nil {
		return nil, err
	}

	m.LimitReached = revisionLimitReached(m.RevisionCount, m.RevisionLimit)

	return &m, nil
}

// GetMilestoneByIDAndProjectIDAndOrganizationID gets a milestone by ID scoped
// to a project and organization. The effective revision limit (milestone
// override, else project default) is resolved via the projects join.
func (r *Repository) GetMilestoneByIDAndProjectIDAndOrganizationID(ctx context.Context, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID) (*Milestone, error) {
	m, err := scanMilestoneRow(func(dest ...any) error {
		return r.DB.QueryRowContext(ctx, `
			SELECT `+milestoneColumns+`
			`+milestoneSelectFrom+`
			WHERE m.id = $1
			AND m.project_id = $2
			AND m.organization_id = $3
		`, milestoneID, projectID, organizationID).Scan(dest...)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrMilestoneNotFound
		}
		return nil, err
	}

	return m, nil
}

// GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate reads a milestone
// with the same scope and locks the row for the duration of the transaction.
// Used by every state-machine transition so concurrent actions serialize on
// the milestone row.
func (r *Repository) GetMilestoneByIDAndProjectIDAndOrganizationIDForUpdate(ctx context.Context, tx *sql.Tx, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID) (*Milestone, error) {
	m, err := scanMilestoneRow(func(dest ...any) error {
		return tx.QueryRowContext(ctx, `
			SELECT `+milestoneColumns+`
			`+milestoneSelectFrom+`
			WHERE m.id = $1
			AND m.project_id = $2
			AND m.organization_id = $3
			FOR UPDATE OF m
		`, milestoneID, projectID, organizationID).Scan(dest...)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrMilestoneNotFound
		}
		return nil, err
	}

	return m, nil
}

// SetMilestoneStatus transitions a milestone to next, guarded by the current
// status (stale writers get zero rows and ErrInvalidStatusTransition).
// completed_at is stamped when the target status is 'completed' and is
// preserved otherwise.
func (r *Repository) SetMilestoneStatus(ctx context.Context, tx *sql.Tx, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID, current MilestoneStatus, next MilestoneStatus) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE milestones
		SET status = $1,
		    completed_at = CASE WHEN $1::milestone_status = 'completed' THEN COALESCE(completed_at, NOW()) ELSE completed_at END,
		    updated_at = NOW()
		WHERE id = $2
		AND project_id = $3
		AND organization_id = $4
		AND status = $5
	`, next, milestoneID, projectID, organizationID, current)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrInvalidStatusTransition
	}

	return nil
}

// SubmitMilestone transitions a milestone to awaiting_approval, increments the
// submission-round counter, and stamps updated_at — guarded by the current
// status.
func (r *Repository) SubmitMilestone(ctx context.Context, tx *sql.Tx, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID, current MilestoneStatus) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE milestones
		SET status = 'awaiting_approval',
		    revision_count = revision_count + 1,
		    updated_at = NOW()
		WHERE id = $1
		AND project_id = $2
		AND organization_id = $3
		AND status = $4
	`, milestoneID, projectID, organizationID, current)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrInvalidStatusTransition
	}

	return nil
}

// CreateMilestoneRevision records one submission round in the revision
// history (append-only; the row is the AI-relevant history artifact).
func (r *Repository) CreateMilestoneRevision(ctx context.Context, tx *sql.Tx, organizationID uuid.UUID, milestoneID uuid.UUID, revisionNumber int, submittedBy uuid.UUID) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO milestone_revisions (
			organization_id,
			milestone_id,
			revision_number,
			submitted_by
		)
		VALUES ($1, $2, $3, $4)
	`, organizationID, milestoneID, revisionNumber, submittedBy)
	if err != nil {
		return err
	}

	return nil
}

// SetMilestonePaymentStatus updates the display-only payment status, guarded
// by the current value.
func (r *Repository) SetMilestonePaymentStatus(ctx context.Context, tx *sql.Tx, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID, current MilestonePaymentStatus, next MilestonePaymentStatus) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE milestones
		SET payment_status = $1,
		    updated_at = NOW()
		WHERE id = $2
		AND project_id = $3
		AND organization_id = $4
		AND payment_status = $5
	`, next, milestoneID, projectID, organizationID, current)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrMilestoneNotFound
	}

	return nil
}

// SetProjectRevisionLimit sets (or clears, when limit is nil) the project-level
// revision limit, scoped to the organization. The caller holds the project row
// FOR UPDATE.
func (r *Repository) SetProjectRevisionLimit(ctx context.Context, tx *sql.Tx, projectID uuid.UUID, organizationID uuid.UUID, limit *int) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE projects
		SET revision_limit = $1,
		    updated_at = NOW()
		WHERE id = $2
		AND organization_id = $3
	`, limit, projectID, organizationID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrProjectNotFound
	}

	return nil
}

// SetMilestoneRevisionLimit sets (or clears, when limit is nil) the
// per-milestone revision-limit override, scoped to the project and
// organization. The caller holds the milestone row FOR UPDATE.
func (r *Repository) SetMilestoneRevisionLimit(ctx context.Context, tx *sql.Tx, milestoneID uuid.UUID, projectID uuid.UUID, organizationID uuid.UUID, limit *int) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE milestones
		SET revision_limit = $1,
		    updated_at = NOW()
		WHERE id = $2
		AND project_id = $3
		AND organization_id = $4
	`, limit, milestoneID, projectID, organizationID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrMilestoneNotFound
	}

	return nil
}

// AssignClient sets (or clears, when clientID is nil) the project's client,
// scoped to the organization.
func (r *Repository) AssignClient(ctx context.Context, tx *sql.Tx, projectID uuid.UUID, organizationID uuid.UUID, clientID *uuid.UUID) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE projects
		SET client_id = $1,
		    updated_at = NOW()
		WHERE id = $2
		AND organization_id = $3
	`, clientID, projectID, organizationID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrProjectNotFound
	}

	return nil
}

// DeleteClientMembership removes the user's membership from the organization
// and returns the deleted membership ID (nil id + nil error when no row
// existed). Used when a client is unassigned or displaced from their last
// project.
func (r *Repository) DeleteClientMembership(ctx context.Context, tx *sql.Tx, organizationID uuid.UUID, userID uuid.UUID) (*uuid.UUID, error) {
	var membershipID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		DELETE FROM memberships
		WHERE organization_id = $1
		AND user_id = $2
		RETURNING id
	`, organizationID, userID).Scan(&membershipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &membershipID, nil
}

// IsClientOnAnyOtherProject reports whether the user is still the assigned
// client of any other project in the organization.
func (r *Repository) IsClientOnAnyOtherProject(ctx context.Context, q Queryer, organizationID uuid.UUID, userID uuid.UUID, exceptProjectID uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM projects
			WHERE organization_id = $1
			AND client_id = $2
			AND id <> $3
		)
	`, organizationID, userID, exceptProjectID).Scan(&exists)
	if err != nil {
		return false, apperrors.ErrDatabase
	}
	return exists, nil
}

// LockClientMembership locks the user's membership row in the organization
// FOR UPDATE. Returns (false, nil) when the row no longer exists (caller
// skips the prune). Serializes the prune of a displaced client against
// concurrent assignment or removal of that client.
func (r *Repository) LockClientMembership(ctx context.Context, tx *sql.Tx, organizationID uuid.UUID, userID uuid.UUID) (bool, error) {
	var id uuid.UUID
	err := tx.QueryRowContext(ctx, `
		SELECT id FROM memberships
		WHERE organization_id = $1 AND user_id = $2
		FOR UPDATE
	`, organizationID, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, apperrors.ErrDatabase
	}
	return true, nil
}

// Queryer is the minimal interface satisfied by both *sql.DB and *sql.Tx.
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CountDeliverables returns the number of deliverables on a milestone.
func (r *Repository) CountDeliverables(ctx context.Context, q Queryer, organizationID uuid.UUID, milestoneID uuid.UUID) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT count(*)
		FROM milestone_deliverables
		WHERE organization_id = $1
		AND milestone_id = $2
	`, organizationID, milestoneID).Scan(&count)
	if err != nil {
		return 0, apperrors.ErrDatabase
	}
	return count, nil
}

// CreateDeliverable inserts a link-based deliverable.
func (r *Repository) CreateDeliverable(ctx context.Context, tx *sql.Tx, d *Deliverable) error {
	err := tx.QueryRowContext(ctx, `
		INSERT INTO milestone_deliverables (
			organization_id,
			milestone_id,
			url,
			title,
			description,
			submitted_by
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, submitted_at
	`, d.OrganizationID, d.MilestoneID, d.URL, d.Title, d.Description, d.SubmittedBy).Scan(&d.ID, &d.SubmittedAt)
	if err != nil {
		return err
	}

	return nil
}

// DeleteDeliverable removes a deliverable scoped to milestone and
// organization. Zero affected rows means the deliverable does not exist.
func (r *Repository) DeleteDeliverable(ctx context.Context, tx *sql.Tx, organizationID uuid.UUID, milestoneID uuid.UUID, deliverableID uuid.UUID) error {
	result, err := tx.ExecContext(ctx, `
		DELETE FROM milestone_deliverables
		WHERE id = $1
		AND organization_id = $2
		AND milestone_id = $3
	`, deliverableID, organizationID, milestoneID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return apperrors.ErrDeliverableNotFound
	}

	return nil
}

// ListDeliverablesByMilestoneID returns a milestone's deliverables, oldest
// first.
func (r *Repository) ListDeliverablesByMilestoneID(ctx context.Context, organizationID uuid.UUID, milestoneID uuid.UUID) ([]Deliverable, error) {
	rows, err := r.DB.QueryContext(ctx, `
		SELECT
			id,
			organization_id,
			milestone_id,
			url,
			title,
			description,
			submitted_by,
			submitted_at
		FROM milestone_deliverables
		WHERE organization_id = $1
		AND milestone_id = $2
		ORDER BY submitted_at ASC
	`, organizationID, milestoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliverables []Deliverable

	for rows.Next() {
		var d Deliverable
		if err := rows.Scan(&d.ID, &d.OrganizationID, &d.MilestoneID, &d.URL, &d.Title, &d.Description, &d.SubmittedBy, &d.SubmittedAt); err != nil {
			return nil, err
		}
		deliverables = append(deliverables, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return deliverables, nil
}

// GetDeliverableByID reads a deliverable scoped to milestone and organization
// (used to capture the before-state for the removal audit).
func (r *Repository) GetDeliverableByID(ctx context.Context, q Queryer, organizationID uuid.UUID, milestoneID uuid.UUID, deliverableID uuid.UUID) (*Deliverable, error) {
	d := &Deliverable{}
	err := q.QueryRowContext(ctx, `
		SELECT
			id,
			organization_id,
			milestone_id,
			url,
			title,
			description,
			submitted_by,
			submitted_at
		FROM milestone_deliverables
		WHERE id = $1
		AND organization_id = $2
		AND milestone_id = $3
	`, deliverableID, organizationID, milestoneID).Scan(&d.ID, &d.OrganizationID, &d.MilestoneID, &d.URL, &d.Title, &d.Description, &d.SubmittedBy, &d.SubmittedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrDeliverableNotFound
		}
		return nil, err
	}
	return d, nil
}

// ListAllMilestonesByProjectID returns every milestone of a project, in
// natural (oldest first) order, with revision and payment fields resolved.
// Used by the approval view.
func (r *Repository) ListAllMilestonesByProjectID(ctx context.Context, organizationID uuid.UUID, projectID uuid.UUID) ([]Milestone, error) {
	rows, err := r.DB.QueryContext(ctx, `
		SELECT `+milestoneColumns+`
		`+milestoneSelectFrom+`
		WHERE m.project_id = $1
		AND m.organization_id = $2
		ORDER BY m.created_at ASC
	`, projectID, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var milestones []Milestone

	for rows.Next() {
		m, err := scanMilestoneRow(func(dest ...any) error { return rows.Scan(dest...) })
		if err != nil {
			return nil, err
		}
		milestones = append(milestones, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return milestones, nil
}

// IsActiveClientMember reports whether the user holds an active membership in
// the organization with the client role, locking the membership row FOR SHARE
// for the duration of the transaction. The lock serializes this check against
// concurrent membership deletion (client.Remove, prune-on-displacement): if a
// concurrent removal commits first, this read re-evaluates against the newest
// row version (row gone -> false -> ErrClientNotFound); if this check acquires
// the lock first, the removal blocks on it until this transaction commits, so
// the project reference is committed before the removal can proceed.
func (r *Repository) IsActiveClientMember(ctx context.Context, tx *sql.Tx, organizationID uuid.UUID, userID uuid.UUID) (bool, error) {
	var membershipID uuid.UUID
	err := tx.QueryRowContext(ctx, `
		SELECT m.id
		FROM memberships m
		JOIN roles r ON r.id = m.role_id
		WHERE m.organization_id = $1
		AND m.user_id = $2
		AND m.status = 'active'
		AND r.name = 'client'
		FOR SHARE OF m
	`, organizationID, userID).Scan(&membershipID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, apperrors.ErrDatabase
	}
	return true, nil
}
