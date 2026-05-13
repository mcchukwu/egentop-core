package validation

import (
	"strings"

	"github.com/mcchukwu/egentop/internal/auth"
)

func ValidateRegisterRequest(req auth.RegisterRequest) Errors {
	errors := Errors{}

	if strings.TrimSpace(req.Password) == "" {
		errors["password"] = "password is required"
	}
	if len(req.Password) < 8 {
		errors["password"] = "password must be at least 8 characters"
	}
	if req.Email == "" && req.Phone == "" {
		errors["identifier"] = "email or phone is required"
	}
	if strings.TrimSpace(req.FirstName) == "" {
		errors["first_name"] = "first name is required"
	}

	return errors
}

func ValidateLoginRequest(req auth.LoginRequest) Errors {
	errors := Errors{}

	if strings.TrimSpace(req.Password) == "" {
		errors["password"] = "password is required"
	}
	if strings.TrimSpace(req.Identifier) == "" {
		errors["identifier"] = "email or phone is required"
	}

	return errors
}
