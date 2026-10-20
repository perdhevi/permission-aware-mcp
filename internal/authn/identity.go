package authn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"permission-aware-mcp/internal/repository"
)

type Identity struct {
	Issuer    string
	Subject   string
	Scopes    []string
	ExpiresAt time.Time
}
type Principal struct {
	UserID   string
	Subject  string
	TenantID string
	Tenant   string
	Scopes   []string
	Roles    []string
}

var ErrUnauthenticated = errors.New("unauthenticated")

type Resolver struct{ users repository.UserRepository }

func NewResolver(users repository.UserRepository) *Resolver { return &Resolver{users: users} }
func (r *Resolver) Resolve(ctx context.Context, id *Identity) (*Principal, error) {
	if id == nil || id.Issuer == "" || id.Subject == "" {
		return nil, ErrUnauthenticated
	}
	u, err := r.users.FindByExternalIdentity(ctx, id.Issuer, id.Subject)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("resolve principal: %w", err)
	}
	if u == nil || !u.Active || u.ID == "" || u.TenantID == "" {
		return nil, ErrUnauthenticated
	}
	return &Principal{UserID: u.ID, Subject: u.Subject, TenantID: u.TenantID, Tenant: u.TenantSlug, Scopes: append([]string{}, id.Scopes...), Roles: append([]string{}, u.Roles...)}, nil
}
