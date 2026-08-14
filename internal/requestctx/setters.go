package requestctx

import (
	"context"

	"github.com/google/uuid"
)

func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return set(ctx, UserIDKey, userID)
}

func WithSessionID(ctx context.Context, sessionID uuid.UUID) context.Context {
	return set(ctx, SessionIDKey, sessionID)
}

func WithOrganizationID(ctx context.Context, organizationID uuid.UUID) context.Context {
	return set(ctx, OrganizationIDKey, organizationID)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return set(ctx, RequestIDKey, requestID)
}

func WithMembershipID(ctx context.Context, membershipID uuid.UUID) context.Context {
	return set(ctx, MembershipIDKey, membershipID)
}

func WithRole(ctx context.Context, role string) context.Context {
	return set(ctx, RoleKey, role)
}

func WithMustChangePassword(ctx context.Context, mustChange bool) context.Context {
	return set(ctx, MustChangePasswordKey, mustChange)
}
