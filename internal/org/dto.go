package org

type CreateOrganizationRequest struct {
	Name string `json:"name" validate:"required"`
	Slug string `json:"slug,omitempty"`
}
