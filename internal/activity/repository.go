package activity

import (
	"context"
	"database/sql"
	"encoding/json"

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

// Create creates a new activity
func (r *Repository) Create(ctx context.Context, tx *sql.Tx, a *Activity) error {
	metadata, err := json.Marshal(a.Metadata)
	if err != nil {
		return apperrors.ErrInternalServer
	}

	query := `
		INSERT INTO activities (
			organization_id,
			project_id,
			milestone_id,
			actor_id,
			type,
			message,
			metadata
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at
	`

	return tx.QueryRowContext(ctx, query,
		a.OrganizationID,
		a.ProjectID,
		a.MilestoneID,
		a.ActorID,
		a.Type,
		a.Message,
		metadata,
	).Scan(
		&a.ID,
		&a.CreatedAt,
	)
}

// List returns the activity feed for an organization, newest first. Rows
// referencing a soft-deleted project render with project_id: null (the
// project row no longer resolves), so the history is preserved without a
// dead project link. The project-scoped feed (ListByProjectID) deliberately
// does not null it — that feed only resolves inside a live project.
func (r *Repository) List(ctx context.Context, orgID uuid.UUID, q pagination.Query) ([]Activity, int, error) {
	var activities []Activity
	var total int

	if err := r.DB.QueryRowContext(ctx, `
		SELECT count(*)
		FROM activities
		WHERE organization_id = $1
	`, orgID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT
			a.id,
			a.organization_id,
			CASE WHEN p.id IS NULL THEN NULL ELSE a.project_id END AS project_id,
			a.milestone_id,
			a.actor_id,
			a.type,
			a.message,
			a.metadata,
			a.created_at,
			u.first_name || ' ' || u.last_name AS actor_name
		FROM activities a
		LEFT JOIN users u ON u.id = a.actor_id
		LEFT JOIN projects p ON p.id = a.project_id AND p.deleted_at IS NULL
		WHERE a.organization_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.DB.QueryContext(ctx, query, orgID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}

	defer rows.Close()

	for rows.Next() {
		var a Activity
		var metadata []byte
		var actorName sql.NullString

		err := rows.Scan(&a.ID, &a.OrganizationID, &a.ProjectID, &a.MilestoneID, &a.ActorID, &a.Type, &a.Message, &metadata, &a.CreatedAt, &actorName)
		if err != nil {
			return nil, 0, err
		}

		if actorName.Valid {
			a.ActorName = &actorName.String
		}

		if err := json.Unmarshal(metadata, &a.Metadata); err != nil {
			a.Metadata = map[string]any{}
		}

		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return activities, total, nil
}

// ListByProjectID returns the activity feed scoped to a single project
// (project-scoped view for clients and staff), newest first.
func (r *Repository) ListByProjectID(ctx context.Context, orgID uuid.UUID, projectID uuid.UUID, q pagination.Query) ([]Activity, int, error) {
	var activities []Activity
	var total int

	if err := r.DB.QueryRowContext(ctx, `
		SELECT count(*)
		FROM activities
		WHERE organization_id = $1
		AND project_id = $2
	`, orgID, projectID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.DB.QueryContext(ctx, `
		SELECT
			a.id,
			a.organization_id,
			a.project_id,
			a.milestone_id,
			a.actor_id,
			a.type,
			a.message,
			a.metadata,
			a.created_at,
			u.first_name || ' ' || u.last_name AS actor_name
		FROM activities a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE a.organization_id = $1
		AND a.project_id = $2
		ORDER BY a.created_at DESC
		LIMIT $3 OFFSET $4
	`, orgID, projectID, q.Limit, q.Offset())
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var a Activity
		var metadata []byte
		var actorName sql.NullString

		err := rows.Scan(&a.ID, &a.OrganizationID, &a.ProjectID, &a.MilestoneID, &a.ActorID, &a.Type, &a.Message, &metadata, &a.CreatedAt, &actorName)
		if err != nil {
			return nil, 0, err
		}

		if actorName.Valid {
			a.ActorName = &actorName.String
		}

		if err := json.Unmarshal(metadata, &a.Metadata); err != nil {
			a.Metadata = map[string]any{}
		}

		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return activities, total, nil
}
