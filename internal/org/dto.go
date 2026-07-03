package org

type CreateOrganizationRequest struct {
	Name string `json:"name" validate:"required"`
	Slug string `json:"slug,omitempty"`
}

type InviteMemberRequest struct {
	Email string `json:"email" validate:"required"`
	Role  Role   `json:"role"`
}

type AddMemberRequest struct {
	UserID string `json:"user_id" validate:"required"`
	Role   Role   `json:"role"`
}

type UpdateMemberRoleRequest struct {
	Role Role `json:"role" validate:"required"`
}
