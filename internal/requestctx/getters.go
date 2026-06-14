package requestctx

import "context"

func UserID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, UserIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func SessionID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, SessionIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func RequestID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, RequestIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func OrganizationID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, OrganizationIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func MembershipID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, MembershipIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func ProjectID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, ProjectIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}

func MilestoneID(ctx context.Context) (string, bool) {
	val, ok := get(ctx, MilestoneIDKey)
	if !ok {
		return "", false
	}

	s, ok := val.(string)
	return s, ok
}
