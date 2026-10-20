package authz

import (
	"context"
	"errors"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/repository"
	"testing"
)

const testUser = "00000000-0000-0000-0000-000000000001"
const testTenant = "00000000-0000-0000-0000-000000000002"
const testDoc = "00000000-0000-0000-0000-000000000003"

type documentSpy struct {
	calls int
	err   error
}

func (s *documentSpy) ListReadable(_ context.Context, tenant, user string, limit int) ([]domain.Source, error) {
	s.calls++
	if tenant != testTenant || user != testUser || limit != 20 {
		panic("wrong authorization parameters")
	}
	return nil, s.err
}
func (s *documentSpy) GetReadable(_ context.Context, tenant, user, id string, max int) (*domain.Document, error) {
	s.calls++
	if tenant != testTenant || user != testUser || id != testDoc || max != 12000 {
		panic("wrong authorization parameters")
	}
	return nil, s.err
}
func TestServiceBoundary(t *testing.T) {
	ctx := context.Background()
	repo := &documentSpy{}
	service := New(repo)
	p := &authn.Principal{UserID: testUser, TenantID: testTenant, Roles: []string{"executive"}}
	for _, invalid := range []*authn.Principal{nil, {}, {UserID: testUser}, {UserID: "spoof", TenantID: testTenant}} {
		if _, err := service.ListAccessibleSources(ctx, invalid, 20); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Fatal(err)
		}
		if _, err := service.GetDocument(ctx, invalid, testDoc, 12000); !errors.Is(err, authn.ErrUnauthenticated) {
			t.Fatal(err)
		}
	}
	for _, limit := range []int{-1, 0, 101} {
		if _, err := service.ListAccessibleSources(ctx, p, limit); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	for _, max := range []int{-1, 0, 50001} {
		if _, err := service.GetDocument(ctx, p, testDoc, max); !errors.Is(err, ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	if _, err := service.GetDocument(ctx, p, "not-a-uuid", 12000); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	if repo.calls != 0 {
		t.Fatal("invalid input reached repository")
	}
	// Claimed roles do not grant access: the database must decide on every call.
	for _, err := range []error{repository.ErrNotFound, errors.New("database unavailable")} {
		repo.err = err
		if _, got := service.ListAccessibleSources(ctx, p, 20); !errors.Is(got, err) {
			t.Fatal(got)
		}
		if _, got := service.GetDocument(ctx, p, testDoc, 12000); !errors.Is(got, err) {
			t.Fatal(got)
		}
	}
	if repo.calls != 4 {
		t.Fatal("reads were cached or skipped")
	}
}
