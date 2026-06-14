package requestctx

import "context"

func WithUserID(ctx context.Context, userID string) context.Context {
	return set(ctx, UserIDKey, userID)
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return set(ctx, SessionIDKey, sessionID)
}

func WithOrganizationID(ctx context.Context, organizationID string) context.Context {
	return set(ctx, OrganizationIDKey, organizationID)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return set(ctx, RequestIDKey, requestID)
}

func WithMembershipID(ctx context.Context, membershipID any) context.Context {
	return set(ctx, MembershipIDKey, membershipID)
}

func WithProjectID(ctx context.Context, projectID string) context.Context {
	return set(ctx, ProjectIDKey, projectID)
}

func WithMilestoneID(ctx context.Context, milestoneID string) context.Context {
	return set(ctx, MilestoneIDKey, milestoneID)
}
