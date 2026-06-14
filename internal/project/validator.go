package project

import (
	"strings"
	"time"

	"github.com/mcchukwu/egentop/internal/apperrors"
)

func (r CreateProjectRequest) Validate() error {

	name := strings.TrimSpace(r.Name)

	if name == "" {
		return apperrors.ErrValidation
	}

	if len(name) < 3 {
		return apperrors.ErrValidation
	}

	if len(name) > 120 {
		return apperrors.ErrValidation
	}

	if len(r.Description) > 2000 {
		return apperrors.ErrValidation
	}

	if r.Priority != "" {
		switch Priority(r.Priority) {
		case PriorityLow,
			PriorityMedium,
			PriorityHigh:
		default:
			return apperrors.ErrValidation
		}
	}

	if r.DueDate != "" {
		_, err := time.Parse(time.RFC3339, r.DueDate)
		if err != nil {
			return apperrors.ErrValidation
		}
	}

	return nil
}
