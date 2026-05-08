package auth

type RegisterRequest struct {
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Password  string `json:"password"`
	LastName  string `json:"last_name"`
	FirstName string `json:"first_name"`
}
