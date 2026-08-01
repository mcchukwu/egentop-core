package organization

type CreateOrganizationRequest struct {
	Name string `json:"name" validate:"required,min=2,max=50"`
	Slug string `json:"slug,omitempty"`
}
