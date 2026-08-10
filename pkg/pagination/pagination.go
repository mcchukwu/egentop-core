package pagination

import (
	"strconv"
)

const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// Query holds validated pagination parameters.
type Query struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
}

// Parse extracts and validates page/limit from raw query string values.
// Empty values fall back to defaults; invalid values are clamped rather than
// rejected so clients can page defensively.
func Parse(pageStr, limitStr string) Query {
	page := DefaultPage
	limit := DefaultLimit

	if v, err := strconv.Atoi(pageStr); err == nil && v > 0 {
		page = v
	}

	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
		limit = v
	}

	if limit > MaxLimit {
		limit = MaxLimit
	}

	return Query{Page: page, Limit: limit}
}

// Offset returns the SQL OFFSET for the query.
func (q Query) Offset() int {
	return (q.Page - 1) * q.Limit
}

// Meta describes the pagination state of a list response.
type Meta struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

// NewMeta builds pagination metadata from a query and a total row count.
func NewMeta(q Query, total int) Meta {
	return Meta{
		Page:  q.Page,
		Limit: q.Limit,
		Total: total,
	}
}

// Response wraps list items and pagination metadata in the documented shape:
//
//	data: {
//	  "items": [...],
//	  "pagination": { "page": 1, "limit": 20, "total": 100 }
//	}
type Response struct {
	Items      any  `json:"items"`
	Pagination Meta `json:"pagination"`
}

// NewResponse builds a paginated response envelope.
func NewResponse(items any, q Query, total int) Response {
	return Response{
		Items:      items,
		Pagination: NewMeta(q, total),
	}
}
