package org

type CreateOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type InviteMemberRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Role   Role   `json:"role"`
}

type UpdateMemberRoleRequest struct {
	Role Role `json:"role"`
}
