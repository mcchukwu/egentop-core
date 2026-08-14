package audit

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
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

// RecordDecision writes an authorization decision to authz_decisions.
// resourceType and resourceID are optional: an empty resourceType / nil
// resourceID is stored as NULL. Services use the resource identity when they
// record scope denials that the RBAC middleware cannot express (e.g. a client
// actor outside their project scope).
func RecordDecision(ctx context.Context, db *sql.DB, organizationID uuid.UUID, userID uuid.UUID, permissionKey string, resourceType string, resourceID uuid.UUID, allowed bool, reason string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO authz_decisions (
			organization_id,
			user_id,
			permission_key,
			resource_type,
			resource_id,
			allowed,
			reason
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, organizationID, userID, permissionKey, nullableString(resourceType), nullableUUID(resourceID), allowed, reason)
	if err != nil {
		return apperrors.ErrDatabase
	}

	return nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func nullableUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}

	return &id
}
