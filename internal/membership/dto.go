package membership

type AddMemberRequest struct {
	UserID string `json:"user_id" validate:"required,uuid"`
	Role   Role   `json:"role" validate:"required"`
}

type InviteMemberRequest struct {
	Email string `json:"email" validate:"required,email"`
	Role  Role   `json:"role" validate:"required"`
}

type UpdateMemberRoleRequest struct {
	Role Role `json:"role" validate:"required"`
}
