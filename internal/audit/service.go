package audit

import (
	"context"
	"database/sql"

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
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (organization_id, user_id, action, metadata)
		VALUES ($1, $2, $3, $4)
	`, entry.OrganizationID, entry.UserID, entry.Action, entry.Metadata)
	if err != nil {
		return apperrors.ErrDatabase
	}

	return nil
}
