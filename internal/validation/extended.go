package validation

import (
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

var ngPhoneRegex = regexp.MustCompile(`^\+234[7-9]\d{9}$`)
var emailRegex = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

func registerCustomValidations() {
	Validate.RegisterValidation("ngphone", validateNGPhone)
	Validate.RegisterValidation("identifier", validateIdentifier)
}

func validateNGPhone(fl validator.FieldLevel) bool {
	phone := fl.Field().String()
	return ngPhoneRegex.MatchString(phone)
}

func validateIdentifier(fl validator.FieldLevel) bool {
	val := strings.TrimSpace(fl.Field().String())

	// email
	if emailRegex.MatchString(val) {
		return true
	}

	// phone (assumes normalized already)
	if ngPhoneRegex.MatchString(val) {
		return true
	}

	return false
}
