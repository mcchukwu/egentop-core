package org

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type MembershipStatus string

const (
	StatusActive    MembershipStatus = "active"
	StatusInvited   MembershipStatus = "invited"
	StatusSuspended MembershipStatus = "suspended"
)

type Membership struct {
	ID             string
	UserID         string
	OrganizationID string
	Role           Role
	Status         MembershipStatus
	CreatedAt      time.Time
}

type Organization struct {
	ID        string
	Name      string
	Slug      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}
