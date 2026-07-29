package activity

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

type Repository struct {
	DB *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		DB: db,
	}
}

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
			metadata,
		)
		VALUES ($1,$2,$3,$4,$5,$6)
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
