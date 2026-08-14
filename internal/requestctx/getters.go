package requestctx

import (
	"context"

	"github.com/google/uuid"
)

func UserID(ctx context.Context) (uuid.UUID, bool) {
	val, ok := get(ctx, UserIDKey)
	if !ok {
		return uuid.Nil, false
	}

	id, ok := val.(uuid.UUID)
	return id, ok
}

func SessionID(ctx context.Context) (uuid.UUID, bool) {
	val, ok := get(ctx, SessionIDKey)
	if !ok {
		return uuid.Nil, false
	}

	id, ok := val.(uuid.UUID)
	return id, ok
}

func OrganizationID(ctx context.Context) (uuid.UUID, bool) {
	val, ok := get(ctx, OrganizationIDKey)
	if !ok {
		return uuid.Nil, false
	}

	id, ok := val.(uuid.UUID)
	return id, ok
}

func MembershipID(ctx context.Context) (uuid.UUID, bool) {
	val, ok := get(ctx, MembershipIDKey)
	if !ok {
		return uuid.Nil, false
	}

	id, ok := val.(uuid.UUID)
	return id, ok
}

func RequestID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, RequestIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func Role(ctx context.Context) (string, bool) {
	val, ok := get(ctx, RoleKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

// MustChangePassword returns the must_change_password flag loaded from the
// user row during authentication.
func MustChangePassword(ctx context.Context) (bool, bool) {
	val, ok := get(ctx, MustChangePasswordKey)
	if !ok {
		return false, false
	}

	b, ok := val.(bool)
	return b, ok
}
