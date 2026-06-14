package apperrors

import "errors"

var (
	// AUTHENTICATION
	ErrInvalidCredentials = errors.New("invalid credentials")

	ErrUnauthorized = errors.New("unauthorized")

	ErrInvalidToken = errors.New("invalid token")

	ErrExpiredToken = errors.New("expired token")

	ErrSessionExpired = errors.New("session expired")

	ErrSessionRevoked = errors.New("session revoked")

	ErrMissingAuthorizationHeader = errors.New("missing authorization header")

	ErrInvalidAuthorizationHeader = errors.New("invalid authorization header")

	ErrInvalidPassword = errors.New("invalid password")

	// AUTHORIZATION
	ErrForbidden = errors.New("forbidden")

	ErrInsufficientPermissions = errors.New("insufficient permissions")

	// USERS
	ErrUserNotFound = errors.New("user not found")

	ErrEmailAlreadyExists = errors.New("email already exists")

	ErrPhoneAlreadyExists = errors.New("phone already exists")

	ErrEmailNotVerified = errors.New("email not verified")

	ErrPhoneNotVerified = errors.New("phone not verified")

	ErrUserSuspended = errors.New("user suspended")

	// ORGANIZATIONS
	ErrOrganizationNotFound = errors.New("organization not found")

	ErrOrganizationSuspended = errors.New("organization suspended")

	ErrOrganizationDeleted = errors.New("organization deleted")

	ErrOrganizationSlugExists = errors.New("organization slug already exists")

	// MEMBERSHIPS
	ErrMembershipNotFound = errors.New("membership not found")

	ErrMembershipRoleNotFound = errors.New("membership role not found")

	ErrAlreadyMember = errors.New("user already belongs to organization")

	ErrInvitationPending = errors.New("invitation already pending")

	// PROJECTS
	ErrProjectNotFound = errors.New("project not found")

	ErrProjectSlugExists = errors.New("project slug already exists")

	ErrProjectStatusNotFound = errors.New("project status not found")

	ErrProjectPriorityNotFound = errors.New("project priority not found")

	ErrInvalidProjectName = errors.New("invalid project name")

	ErrInvalidProjectDescription = errors.New("invalid project description")

	ErrInvalidProjectStatusTransition = errors.New("invalid project status transition")

	ErrMilestoneNotFound = errors.New("milestone not found")

	ErrInvalidMilestoneName = errors.New("invalid milestone name")

	ErrInvalidMilestoneDescription = errors.New("invalid milestone description")

	ErrInvalidMilestoneStatusTransition = errors.New("invalid milestone status transition")

	ErrInvalidProjectPriority = errors.New("invalid project priority")

	ErrInvalidMilestonePriority = errors.New("invalid milestone priority")

	ErrInvalidDueDate = errors.New("invalid due date")

	// VALIDATION
	ErrValidation = errors.New("validation error")

	ErrInvalidRequestBody = errors.New("invalid request body")

	ErrMissingRequiredField = errors.New("missing required field")

	ErrInvalidEmail = errors.New("invalid email")

	ErrInvalidStatusTransition = errors.New("invalid status transition")

	ErrWeakPassword = errors.New("weak password")

	// RATE LIMITING
	ErrRateLimited = errors.New("too many requests")

	// SYSTEM
	ErrInternalServer = errors.New("internal server error")

	ErrDatabase = errors.New("database error")
)
