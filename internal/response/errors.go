package response

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Success bool        `json:"success"`
	Error   ErrorDetail `json:"error"`
}

type ValidationErrorResponse struct {
	Success bool `json:"success"`
	Error   struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func Error(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(status)

	json.NewEncoder(w).Encode(ErrorResponse{
		Success: false,
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}

func ValidationError(w http.ResponseWriter, fields map[string]string) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusBadRequest)

	json.NewEncoder(w).Encode(ValidationErrorResponse{
		Success: false,
		Error: struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		}{
			Code:    "validation_error",
			Message: "validation failed",
			Fields:  fields,
		},
	})
}

func HandleError(w http.ResponseWriter, err error) {
	switch {
	// AUTH
	case errors.Is(err, apperrors.ErrInvalidCredentials):
		Error(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	case errors.Is(err, apperrors.ErrUnauthorized):
		Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	case errors.Is(err, apperrors.ErrForbidden):
		Error(w, http.StatusForbidden, "forbidden", "access denied")
	case errors.Is(err, apperrors.ErrSessionExpired):
		Error(w, http.StatusUnauthorized, "session_expired", "session expired")
	case errors.Is(err, apperrors.ErrSessionRevoked):
		Error(w, http.StatusUnauthorized, "session_revoked", "session revoked")
	case errors.Is(err, apperrors.ErrInvalidToken):
		Error(w, http.StatusUnauthorized, "invalid_token", "invalid token")
	case errors.Is(err, apperrors.ErrInvalidPassword):
		Error(w, http.StatusUnauthorized, "invalid_password", "invalid password")
	case errors.Is(err, apperrors.ErrWeakPassword):
		Error(w, http.StatusBadRequest, "weak_password", "weak password")
	case errors.Is(err, apperrors.ErrInsufficientPermissions):
		Error(w, http.StatusForbidden, "insufficient_permissions", "insufficient permissions")
	case errors.Is(err, apperrors.ErrPasswordChangeRequired):
		Error(w, http.StatusForbidden, "password_change_required", "you must change your password before continuing")

		// USERS
	case errors.Is(err, apperrors.ErrUserNotFound):
		Error(w, http.StatusConflict, "user_not_found", "user not found")
	case errors.Is(err, apperrors.ErrEmailAlreadyExists):
		Error(w, http.StatusConflict, "email_already_exists", "email already exists")
	case errors.Is(err, apperrors.ErrPhoneAlreadyExists):
		Error(w, http.StatusConflict, "phone_already_exists", "phone already exists")

	// ORGS
	case errors.Is(err, apperrors.ErrOrganizationNotFound):
		Error(w, http.StatusNotFound, "organization_not_found", "organization not found")
	case errors.Is(err, apperrors.ErrOrganizationSuspended):
		Error(w, http.StatusForbidden, "organization_suspended", "organization suspended")
	case errors.Is(err, apperrors.ErrOrganizationIDInvalid):
		Error(w, http.StatusBadRequest, "invalid_organization_id", "organization id is invalid")
	case errors.Is(err, apperrors.ErrOrganizationSlugExists):
		Error(w, http.StatusConflict, "organization_slug_exists", "organization slug already exists")

	// MEMBERSHIPS
	case errors.Is(err, apperrors.ErrMembershipNotFound):
		Error(w, http.StatusNotFound, "membership_not_found", "membership not found")
	case errors.Is(err, apperrors.ErrAlreadyMember):
		Error(w, http.StatusConflict, "already_member", "user already belongs to organization")
	case errors.Is(err, apperrors.ErrInvitationPending):
		Error(w, http.StatusConflict, "invitation_pending", "invitation already pending")

	// PROJECTS / MILESTONES / ASSIGNMENTS
	case errors.Is(err, apperrors.ErrProjectNotFound):
		Error(w, http.StatusNotFound, "project_not_found", "project not found")
	case errors.Is(err, apperrors.ErrMilestoneNotFound):
		Error(w, http.StatusNotFound, "milestone_not_found", "milestone not found")
	case errors.Is(err, apperrors.ErrAssignmentNotFound):
		Error(w, http.StatusNotFound, "assignment_not_found", "assignment not found")
	case errors.Is(err, apperrors.ErrClientNotFound):
		Error(w, http.StatusNotFound, "client_not_found", "client not found")
	case errors.Is(err, apperrors.ErrDeliverableNotFound):
		Error(w, http.StatusNotFound, "deliverable_not_found", "deliverable not found")
	case errors.Is(err, apperrors.ErrInvalidStatusTransition):
		Error(w, http.StatusBadRequest, "invalid_status_transition", "invalid status transition")
	case errors.Is(err, apperrors.ErrInvalidDueDate):
		Error(w, http.StatusBadRequest, "invalid_due_date", "invalid due date")
	case errors.Is(err, apperrors.ErrProjectHasNoClient):
		Error(w, http.StatusBadRequest, "project_has_no_client", "project has no client")
	case errors.Is(err, apperrors.ErrDeliverableRequired):
		Error(w, http.StatusBadRequest, "deliverable_required", "at least one deliverable is required before submission")
	case errors.Is(err, apperrors.ErrMilestoneNotAwaitingApproval):
		Error(w, http.StatusConflict, "milestone_not_awaiting_approval", "milestone is not awaiting approval")
	case errors.Is(err, apperrors.ErrClientAttachedToProject):
		Error(w, http.StatusConflict, "client_attached_to_project", "client membership is attached to a project and cannot be removed directly")

	// VALIDATION
	case errors.Is(err, apperrors.ErrValidation):
		Error(w, http.StatusBadRequest, "validation_error", "validation failed")
	case errors.Is(err, apperrors.ErrInvalidRequestBody):
		Error(w, http.StatusBadRequest, "invalid_request_body", "request body is invalid")
	case errors.Is(err, apperrors.ErrUserIdentifierInvalid):
		Error(w, http.StatusBadRequest, "invalid_identifier", "phone or email is invalid")
	case errors.Is(err, apperrors.ErrInvalidProjectPriority):
		Error(w, http.StatusBadRequest, "invalid_project_priority", "invalid project priority")

	// RATE LIMIT
	case errors.Is(err, apperrors.ErrRateLimited):
		Error(w, http.StatusTooManyRequests, "rate_limited", "too many requests")

	// METHOD / SYSTEM
	case errors.Is(err, apperrors.ErrMethodNotAllowed):
		Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")

	// DEFAULT
	default:
		Error(w, http.StatusInternalServerError, "internal_server_error", "internal server error")
	}
}
