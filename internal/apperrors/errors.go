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

	ErrPhoneNotVerified = errors.New("phone not verified")

	ErrUserSuspended = errors.New("user suspended")

	ErrUserIdentifierInvalid = errors.New("phone or email is invalid")

	// ORGANIZATIONS
	ErrOrganizationIDInvalid = errors.New("organization id is invalid")

	ErrOrganizationNotFound = errors.New("organization not found")

	ErrOrganizationSuspended = errors.New("organization suspended")

	ErrOrganizationDeleted = errors.New("organization deleted")

	ErrOrganizationSlugExists = errors.New("organization slug already exists")

	ErrOrganizationNameInvalid = errors.New("organization name is invalid")

	// MEMBERSHIPS
	ErrMembershipNotFound = errors.New("membership not found")

	ErrMembershipRoleNotFound = errors.New("membership role not found")

	ErrAlreadyMember = errors.New("user already belongs to organization")

	ErrInvitationPending = errors.New("invitation already pending")

	// ErrPersonalWorkspace is returned by staff-membership mutations on a
	// registration-created personal workspace. It maps to 409 Conflict (the
	// actor isn't lacking permission — even the owner is blocked), NOT 403.
	ErrPersonalWorkspace = errors.New("personal workspace does not accept staff members")

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

	// ErrDueDateInPast is returned by the service for update paths that would
	// set a due date before today (UTC date). The handler maps it to the
	// validation_error contract with a fields.DueDate error, keeping the 404
	// (deleted project) and 400 (frozen project) precedences intact.
	ErrDueDateInPast = errors.New("due date can't be in the past")

	// LAYER-1 (client role, approvals, revisions, deliverables, payments)
	ErrProjectHasNoClient = errors.New("project has no client")

	ErrDeliverableRequired = errors.New("at least one deliverable is required")

	ErrClientNotFound = errors.New("client not found")

	ErrDeliverableNotFound = errors.New("deliverable not found")

	ErrMilestoneNotAwaitingApproval = errors.New("milestone is not awaiting approval")

	ErrClientAttachedToProject = errors.New("client membership is attached to a project")

	ErrPasswordChangeRequired = errors.New("password change required")

	// ASSIGNMENTS
	ErrAssignmentNotFound = errors.New("assignment not found")

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
	ErrInternalServer   = errors.New("internal server error")
	ErrMethodNotAllowed = errors.New("method not allowed")

	ErrDatabase = errors.New("database error")
)
