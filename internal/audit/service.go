package audit

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

type AuditService struct {
	DB *sql.DB
}

func NewAuditService(dbConn *sql.DB) *AuditService {
	return &AuditService{
		DB: dbConn,
	}
}

func (s *AuditService) Log(ctx context.Context, tx *sql.Tx, entry LogEntry) error {
	if entry.OrganizationID == nil {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.UserID == nil {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.Action == "" {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.EntityType == "" {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.EntityID == nil {
		return apperrors.ErrInvalidRequestBody
	}

	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}

	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		return apperrors.ErrInternalServer
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO audit_logs (
			organization_id, 
			user_id, 
			action, 
			entity_type, 
			entity_id, 
			metadata
		)
		VALUES ($1, $2, $3, $4)
	`, entry.OrganizationID, entry.UserID, entry.Action, entry.EntityType, entry.EntityID, metadataJSON)
	if err != nil {
		return apperrors.ErrDatabase
	}

	return nil
}
