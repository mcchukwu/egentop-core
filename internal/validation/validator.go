package validation

import "github.com/go-playground/validator/v10"

var Validate *validator.Validate

// Init initializes the validator
func Init() {
	Validate = validator.New()

	registerCustomValidations()
}

// ValidateStruct validates a struct
func ValidateStruct(data any) map[string]string {
	err := Validate.Struct(data)
	if err == nil {
		return nil
	}

	validationErrors := err.(validator.ValidationErrors)

	fields := map[string]string{}

	for _, e := range validationErrors {
		fields[e.Field()] = mapValidationMessage(e)
	}

	return fields
}

// mapValidationMessage maps the validation error message
func mapValidationMessage(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return "field is required"
	case "email":
		return "must be a valid email"
	case "min":
		return "value is too short"
	case "max":
		return "value is too long"
	case "ngphone":
		return "must be a valid Nigerian phone number"
	case "identifier":
		return "must be a valid email or phone number"

	default:
		return "invalid value"
	}
}
