package authn

import (
	"context"
	"errors"
	"testing"

	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/repository"
)

type userRepo struct {
	user            *domain.User
	err             error
	issuer, subject string
}

func (r *userRepo) FindByExternalIdentity(_ context.Context, issuer, subject string) (*domain.User, error) {
	r.issuer = issuer
	r.subject = subject
	return r.user, r.err
}
func TestResolve(t *testing.T) {
	dbErr := errors.New("database offline")
	for _, tc := range []struct {
		name           string
		user           *domain.User
		err, errorWant error
	}{
		{name: "known", user: &domain.User{ID: "alice-id", Subject: "alice", TenantID: "tenant-id", TenantSlug: "acme", Active: true}},
		{name: "unknown", err: repository.ErrNotFound, errorWant: ErrUnauthenticated},
		{name: "inactive", user: &domain.User{ID: "alice-id", TenantID: "tenant-id", Active: false}, errorWant: ErrUnauthenticated},
		{name: "database failure", err: dbErr, errorWant: dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &userRepo{user: tc.user, err: tc.err}
			p, err := NewResolver(repo).Resolve(context.Background(), &Identity{Issuer: "http://dev.local", Subject: "alice", Scopes: []string{"knowledge:read"}})
			if tc.errorWant != nil {
				if !errors.Is(err, tc.errorWant) {
					t.Fatalf("got %v", err)
				}
				return
			}
			if err != nil || p.UserID != "alice-id" || p.Tenant != "acme" || repo.issuer != "http://dev.local" || repo.subject != "alice" {
				t.Fatalf("principal=%+v err=%v", p, err)
			}
		})
	}
}
