package middleware

import (
	"context"

	"github.com/mcchukwu/egentop/internal/org"
)

func GetUserID(ctx context.Context) string {
	userID, _ := ctx.Value(UserIDKey).(string)
	return userID
}

func GetSessionID(ctx context.Context) string {
	sessionID, _ := ctx.Value(SessionIDKey).(string)
	return sessionID
}

func GetOrganization(ctx context.Context) *org.Organization {
	org, _ := ctx.Value(OrganizationKey).(*org.Organization)
	return org
}

func GetMembership(ctx context.Context) *org.Membership {
	membership, _ := ctx.Value(MembershipKey).(*org.Membership)
	return membership
}
