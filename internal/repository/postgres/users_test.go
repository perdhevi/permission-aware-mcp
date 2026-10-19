package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/repository"
)

func TestUsersIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewUsers(pool)
	for _, subject := range []string{"alice", "bob", "carol", "dave"} {
		u, err := repo.FindByExternalIdentity(ctx, "http://dev.local", subject)
		if err != nil || u.Subject != subject || u.TenantSlug != "acme" || !u.Active {
			t.Fatalf("%s: %+v %v", subject, u, err)
		}
	}
	for _, pair := range [][2]string{{"http://dev.local", "nonexistent"}, {"http://wrong.local", "alice"}} {
		if _, err := repo.FindByExternalIdentity(ctx, pair[0], pair[1]); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("want not found, got %v", err)
		}
	}
	// Dedicated test identity; never modify seeded users.
	subject := "inactive-test-" + time.Now().Format("150405.000000000")
	_, err = pool.Exec(ctx, `INSERT INTO users(tenant_id,issuer,subject,display_name,active) SELECT id,'http://dev.local',$1,'Test',false FROM tenants WHERE slug='acme'`, subject)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE issuer='http://dev.local' AND subject=$1`, subject)
		if err != nil {
			t.Error(err)
		}
	}()
	if _, err := authn.NewResolver(repo).Resolve(ctx, &authn.Identity{Issuer: "http://dev.local", Subject: subject}); !errors.Is(err, authn.ErrUnauthenticated) {
		t.Fatalf("inactive user accepted: %v", err)
	}
	var extension string
	if err := pool.QueryRow(ctx, `SELECT extname FROM pg_extension WHERE extname='vector'`).Scan(&extension); err != nil {
		t.Fatal(err)
	}
}
