package client

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/mcchukwu/egentop/internal/activity"
	"github.com/mcchukwu/egentop/internal/apperrors"
	"github.com/mcchukwu/egentop/internal/audit"
	"github.com/mcchukwu/egentop/internal/membership"
	"github.com/mcchukwu/egentop/pkg/db"
	"github.com/mcchukwu/egentop/pkg/pagination"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	DB              *sql.DB
	Repo            *Repository
	AuditService    *audit.Service
	ActivityService *activity.Service
}

func NewService(db *sql.DB, repo *Repository, auditService *audit.Service, activityService *activity.Service) *Service {
	return &Service{
		DB:              db,
		Repo:            repo,
		AuditService:    auditService,
		ActivityService: activityService,
	}
}

// Provision provisions a client account in the organization:
//   - existing user, not a member   -> reuse the user, create an active client
//     membership, issue NO credential (must_change_password stays false)
//   - existing user, already member -> 409 already_member
//   - no user                       -> create the user with a one-time
//     credential, must_change_password = true, return the credential once
//
// Provisioning never creates a default organization and never registers a
// session. The actor is a staff member with client.provision.
func (s *Service) Provision(ctx context.Context, actorID uuid.UUID, orgID uuid.UUID, req ProvisionRequest) (*ProvisionResult, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *ProvisionResult

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		clientRoleID, err := membership.ResolveSystemRoleID(dbCtx, tx, membership.RoleClient)
		if err != nil {
			return err
		}

		// Reuse an existing user when the identifier matches.
		existing, err := s.Repo.FindUserByIdentifier(dbCtx, tx, req.Email, req.Phone)
		if err == nil {
			isMember, err := s.Repo.IsMember(dbCtx, tx, existing.ID, orgID)
			if err != nil {
				return err
			}
			if isMember {
				return apperrors.ErrAlreadyMember
			}

			if _, err := s.Repo.CreateClientMembership(dbCtx, tx, existing.ID, orgID, clientRoleID); err != nil {
				// Concurrent double-provision: the loser hits the
				// (user_id, organization_id) unique index even though the
				// pre-check saw no membership. Surface 409, never 500.
				if db.IsUniqueViolation(err) {
					return apperrors.ErrAlreadyMember
				}
				return err
			}

			result = &ProvisionResult{
				ClientID: existing.ID,
				Email:    existing.Email,
				Phone:    existing.Phone,
			}

			return s.logProvision(dbCtx, tx, actorID, orgID, existing.ID, false)
		}
		if !errors.Is(err, apperrors.ErrUserNotFound) {
			return err
		}

		// No matching user: create one with a one-time credential.
		if req.FirstName == "" || req.LastName == "" {
			return apperrors.ErrValidation
		}

		oneTime, err := GenerateOneTimePassword()
		if err != nil {
			return apperrors.ErrInternalServer
		}

		hash, err := HashPassword(oneTime)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		userID, err := s.Repo.CreateUser(dbCtx, tx, nullableString(req.Email), nullableString(req.Phone), req.FirstName, req.LastName, hash)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return uniqueIdentifierError(err)
			}
			return err
		}

		if _, err := s.Repo.CreateClientMembership(dbCtx, tx, userID, orgID, clientRoleID); err != nil {
			if db.IsUniqueViolation(err) {
				return apperrors.ErrAlreadyMember
			}
			return err
		}

		result = &ProvisionResult{
			ClientID:         userID,
			Email:            nullableString(req.Email),
			Phone:            nullableString(req.Phone),
			CredentialIssued: true,
			OneTimePassword:  oneTime,
		}

		return s.logProvision(dbCtx, tx, actorID, orgID, userID, true)
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Service) logProvision(ctx context.Context, tx *sql.Tx, actorID uuid.UUID, orgID uuid.UUID, clientID uuid.UUID, credentialIssued bool) error {
	if err := s.AuditService.Log(ctx, tx, audit.LogEntry{
		OrganizationID: &orgID,
		UserID:         &actorID,
		Action:         "client.provisioned",
		EntityType:     "client",
		EntityID:       &clientID,
		Metadata: audit.VersionedMetadata(
			nil,
			map[string]any{
				"client_id":         clientID,
				"credential_issued": credentialIssued,
			},
			"",
		),
	}); err != nil {
		return err
	}

	act := activity.NewActivity(orgID, actorID, nil, nil, activity.ActivityClientProvisioned, "Client provisioned", map[string]any{
		"client_id":         clientID,
		"credential_issued": credentialIssued,
	})
	return s.ActivityService.Log(ctx, tx, act)
}

// ResetCredential rotates a client's one-time credential (Security HIGH).
// All in one transaction:
//   - target must be an active client-role member of the org (else 404)
//   - fresh one-time credential, bcrypt cost 12, must_change_password = true
//   - ALL of the client's sessions are revoked: a client still logged in with
//     the old credential must not retain access after rotation
//
// The new credential is returned exactly once.
func (s *Service) ResetCredential(ctx context.Context, actorID uuid.UUID, orgID uuid.UUID, userID uuid.UUID) (*ProvisionResult, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	var result *ProvisionResult

	err := db.WithTransaction(dbCtx, s.DB, func(tx *sql.Tx) error {
		clientUser, err := s.Repo.GetClientUser(dbCtx, tx, orgID, userID)
		if err != nil {
			return err
		}

		oneTime, err := GenerateOneTimePassword()
		if err != nil {
			return apperrors.ErrInternalServer
		}

		hash, err := HashPassword(oneTime)
		if err != nil {
			return apperrors.ErrInternalServer
		}

		if err := s.Repo.RotateCredential(dbCtx, tx, userID, hash); err != nil {
			return err
		}
		if err := s.Repo.RevokeAllSessions(dbCtx, tx, userID); err != nil {
			return err
		}

		if err := s.AuditService.Log(dbCtx, tx, audit.LogEntry{
			OrganizationID: &orgID,
			UserID:         &actorID,
			Action:         "client.credential_rotated",
			EntityType:     "client",
			EntityID:       &userID,
			Metadata: audit.VersionedMetadata(
				nil,
				map[string]any{
					"client_id":            userID,
					"credential_issued":    true,
					"must_change_password": true,
					"sessions_revoked":     true,
				},
				"",
			),
		}); err != nil {
			return err
		}

		act := activity.NewActivity(orgID, actorID, nil, nil, activity.ActivityClientCredentialRotated, "Client credential rotated", map[string]any{
			"client_id": userID,
		})
		if err := s.ActivityService.Log(dbCtx, tx, act); err != nil {
			return err
		}

		result = &ProvisionResult{
			ClientID:         clientUser.UserID,
			Email:            clientUser.Email,
			Phone:            clientUser.Phone,
			CredentialIssued: true,
			OneTimePassword:  oneTime,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// List returns the organization's clients (client-role memberships only),
// newest first.
func (s *Service) List(ctx context.Context, orgID uuid.UUID, q pagination.Query) ([]Client, pagination.Meta, error) {
	dbCtx, cancel := db.WithDBTimeout(ctx)
	defer cancel()

	clients, total, err := s.Repo.ListClients(dbCtx, orgID, q)
	if err != nil {
		return nil, pagination.Meta{}, err
	}

	return clients, pagination.NewMeta(q, total), nil
}

// oneTimePasswordAlphabet omits visually ambiguous characters (0/O, 1/l/I).
const oneTimePasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"

const oneTimePasswordLength = 16

// GenerateOneTimePassword returns a cryptographically random, uniformly
// distributed alphanumeric credential (rejection sampling avoids modulo
// bias). ~93 bits of entropy; the client must rotate it before acting
// (must_change_password gate).
func GenerateOneTimePassword() (string, error) {
	const alphabetSize = len(oneTimePasswordAlphabet)
	const maxByte = 256 - (256 % alphabetSize) // largest multiple of alphabetSize below 256

	out := make([]byte, oneTimePasswordLength)
	buf := make([]byte, oneTimePasswordLength)

	for filled := 0; filled < oneTimePasswordLength; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, c := range buf {
			if int(c) < maxByte {
				out[filled] = oneTimePasswordAlphabet[int(c)%alphabetSize]
				filled++
				if filled == oneTimePasswordLength {
					break
				}
			}
		}
	}

	return string(out), nil
}

// HashPassword hashes a password with bcrypt cost 12 (the project standard).
func HashPassword(pw string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(bytes), err
}

func uniqueIdentifierError(err error) error {
	// Constraint names mirror the auth package's matching: email_/phone_ unique
	// constraints on users. Falls back to a database error when the violated
	// constraint is not one of ours.
	var detail string
	if pgErr := db.AsPgError(err); pgErr != nil {
		detail = pgErr.ConstraintName
	}
	switch {
	case strings.Contains(detail, "email"):
		return apperrors.ErrEmailAlreadyExists
	case strings.Contains(detail, "phone"):
		return apperrors.ErrPhoneAlreadyExists
	default:
		return apperrors.ErrDatabase
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
