package middleware

type contextKey string

const (
	UserIDKey       contextKey = "user_id"
	SessionIDKey    contextKey = "session_id"
	OrganizationKey contextKey = "organization"
)
