package domain

type User struct {
	ID          string
	TenantID    string
	TenantSlug  string
	Subject     string
	DisplayName string
	Active      bool
	Roles       []string
}
