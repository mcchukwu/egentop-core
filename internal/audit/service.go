package audit

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

type Service struct {
	DB *sql.DB
}

func NewService(dbConn *sql.DB) *Service {
	return &Service{
		DB: dbConn,
	}
}

func (s *Service) Log(ctx context.Context, tx *sql.Tx, entry LogEntry) error {
	if entry.Action == "" {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}

	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return apperrors.ErrInternalServer
	}

	exec := func(ctx context.Context, query string, args ...any) error {
		if tx != nil {
			_, err = tx.ExecContext(ctx, query, args...)
			return err
		}

		_, err = s.DB.ExecContext(ctx, query, args...)
		return err
	}

	err = exec(ctx, `
		INSERT INTO audit_logs (
			organization_id, 
			user_id, 
			action, 
			entity_type, 
			entity_id, 
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, entry.OrganizationID, entry.UserID, entry.Action, entry.EntityType, entry.EntityID, metadataJSON)
	if err != nil {
		return apperrors.ErrDatabase
	}

	return nil
}
