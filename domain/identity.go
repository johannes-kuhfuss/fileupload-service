package domain

type Identity struct {
	Subject  string
	TenantID string
	Scopes   []string
}
