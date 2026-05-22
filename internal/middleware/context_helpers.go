package middleware

import (
	"context"

	"github.com/mcchukwu/egentop/internal/organization"
)

func GetUserID(ctx context.Context) string {
	userID, _ := ctx.Value(UserIDKey).(string)
	return userID
}

func GetSessionID(ctx context.Context) string {
	sessionID, _ := ctx.Value(SessionIDKey).(string)
	return sessionID
}

func GetOrganization(ctx context.Context) *organization.Organization {
	org, _ := ctx.Value(OrganizationKey).(*organization.Organization)
	return org
}
