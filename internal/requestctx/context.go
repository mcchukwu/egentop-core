package requestctx

import "context"

type contextKey string

const (
	UserIDKey         contextKey = "user_id"
	SessionIDKey      contextKey = "session_id"
	OrganizationIDKey contextKey = "organization_id"
	RequestIDKey      contextKey = "request_id"
	MembershipIDKey   contextKey = "membership_id"
	RoleKey           contextKey = "role"
)

func set(ctx context.Context, key contextKey, value any) context.Context {
	return context.WithValue(ctx, key, value)
}

func get(ctx context.Context, key contextKey) (any, bool) {
	val := ctx.Value(key)
	if val == nil {
		return nil, false
	}

	return val, true
}
