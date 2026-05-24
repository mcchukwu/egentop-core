package response

type Pagination struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

type PaginatedData struct {
	Items      any        `json:"items"`
	Pagination Pagination `json:"pagination"`
}
