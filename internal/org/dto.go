package org

type CreateOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type InviteMemberRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}
