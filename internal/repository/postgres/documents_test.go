package postgres

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/repository"
)

func TestDocumentsIntegration(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := authz.New(NewDocuments(pool))
	resolver := authn.NewResolver(NewUsers(pool))
	ids := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT source,id::text FROM documents WHERE tenant_id=(SELECT id FROM tenants WHERE slug='acme')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var source, id string
		if err := rows.Scan(&source, &id); err != nil {
			t.Fatal(err)
		}
		ids[source] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	matrix := map[string][]string{
		"alice": {"Company Handbook", "Engineering Roadmap"}, "bob": {"Company Handbook", "Salary Guidelines"},
		"carol": {"Board Strategy", "Company Handbook", "Engineering Roadmap", "Salary Guidelines"}, "dave": {"Company Handbook"},
	}
	for subject, want := range matrix {
		p, err := resolver.Resolve(ctx, &authn.Identity{Issuer: "http://dev.local", Subject: subject})
		if err != nil {
			t.Fatal(err)
		}
		sources, err := service.ListAccessibleSources(ctx, p, 100)
		if err != nil {
			t.Fatal(err)
		}
		titles := []string{}
		allowed := map[string]bool{}
		for _, s := range sources {
			titles = append(titles, s.Title)
			allowed[s.DocumentID] = true
		}
		if !reflect.DeepEqual(titles, want) {
			t.Fatalf("%s: %v, want %v", subject, titles, want)
		}
		limited, err := service.ListAccessibleSources(ctx, p, 1)
		if err != nil || len(limited) != 1 || limited[0].Title != want[0] {
			t.Fatalf("%s limit/order: %v %v", subject, limited, err)
		}
		for _, id := range ids {
			doc, err := service.GetDocument(ctx, p, id, 5)
			if allowed[id] {
				if err != nil || !doc.Truncated || len([]rune(doc.Content)) != 5 {
					t.Fatalf("%s: %+v %v", subject, doc, err)
				}
			} else if !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("%s read forbidden document: %v", subject, err)
			}
		}
		if _, err := service.GetDocument(ctx, p, "00000000-0000-0000-0000-000000000000", 12000); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(err)
		}
	}
	// Use isolated fixtures and remove them without modifying demo identities.
	var tenant, other, user, role, doc string
	err = pool.QueryRow(ctx, `INSERT INTO tenants(slug,name) VALUES('phase2-'||gen_random_uuid(),'Test') RETURNING id::text`).Scan(&tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM documents WHERE tenant_id=$1::uuid OR tenant_id=$2::uuid`, `DELETE FROM users WHERE tenant_id=$1::uuid OR tenant_id=$2::uuid`, `DELETE FROM roles WHERE tenant_id=$1::uuid OR tenant_id=$2::uuid`, `DELETE FROM tenants WHERE id=$1::uuid OR id=$2::uuid`} {
			// other starts as a zero UUID so cleanup also works after partial setup.
			if other == "" {
				other = "00000000-0000-0000-0000-000000000000"
			}
			if _, err := pool.Exec(cleanup, q, tenant, other); err != nil {
				t.Error(err)
			}
		}
	}()
	mustScan := func(q string, dest *string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, q, args...).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustScan(`INSERT INTO tenants(slug,name) VALUES('phase2-other-'||gen_random_uuid(),'Other') RETURNING id::text`, &other)
	mustScan(`INSERT INTO users(tenant_id,issuer,subject,display_name) VALUES($1,'test',gen_random_uuid()::text,'Test') RETURNING id::text`, &user, tenant)
	mustScan(`INSERT INTO roles(tenant_id,name) VALUES($1,'executive') RETURNING id::text`, &role, tenant)
	mustScan(`INSERT INTO documents(tenant_id,title,source,content,classification) VALUES($1,'Private','private','A🦊é界Z','restricted') RETURNING id::text`, &doc, tenant)
	p := &authn.Principal{UserID: user, TenantID: tenant, Roles: []string{"executive"}}
	denied := func() {
		t.Helper()
		if _, err := service.GetDocument(ctx, p, doc, 12000); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("expected denial: %v", err)
		}
		sources, err := service.ListAccessibleSources(ctx, p, 20)
		if err != nil || len(sources) != 0 {
			t.Fatalf("metadata leaked: %v %v", sources, err)
		}
	}
	denied() // No implicit executive privilege and no ACL means no access.
	exec(`INSERT INTO document_acl(tenant_id,document_id,user_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, user)
	got, err := service.GetDocument(ctx, p, doc, 3)
	if err != nil || got.Content != "A🦊é" || !got.Truncated {
		t.Fatalf("Unicode truncation: %+v %v", got, err)
	}
	got, err = service.GetDocument(ctx, p, doc, 5)
	if err != nil || got.Truncated {
		t.Fatalf("exact bound: %+v %v", got, err)
	}
	exec(`DELETE FROM document_acl WHERE document_id=$1`, doc)
	denied()
	exec(`INSERT INTO document_acl(tenant_id,document_id,role_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, role)
	denied()
	exec(`INSERT INTO user_roles(tenant_id,user_id,role_id) VALUES($1,$2,$3)`, tenant, user, role)
	if _, err := service.GetDocument(ctx, p, doc, 12000); err != nil {
		t.Fatal(err)
	}
	// Overlapping user/role grants must not duplicate a listed source.
	exec(`INSERT INTO document_acl(tenant_id,document_id,user_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, user)
	sources, err := service.ListAccessibleSources(ctx, p, 20)
	if err != nil || len(sources) != 1 {
		t.Fatalf("duplicate grants: %v %v", sources, err)
	}
	exec(`DELETE FROM document_acl WHERE document_id=$1 AND user_id=$2`, doc, user)
	exec(`DELETE FROM document_acl WHERE document_id=$1 AND role_id=$2`, doc, role)
	denied()
	exec(`INSERT INTO document_acl(tenant_id,document_id,role_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, role)
	exec(`UPDATE users SET active=false WHERE id=$1`, user)
	denied()
	exec(`UPDATE users SET active=true WHERE id=$1`, user)
	exec(`DELETE FROM user_roles WHERE user_id=$1`, user)
	denied()
	// Cross-tenant principals never see content or metadata, even with a known UUID.
	p.TenantID = other
	denied()
	p.TenantID = tenant
	var foreignDoc string
	mustScan(`INSERT INTO documents(tenant_id,title,source,content,classification) VALUES($1,'Foreign secret','foreign','secret','restricted') RETURNING id::text`, &foreignDoc, other)
	if _, err := service.GetDocument(ctx, p, foreignDoc, 12000); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO user_roles(tenant_id,user_id,role_id) VALUES($1,$2,$3)`, []any{other, user, role}},
		{`INSERT INTO document_acl(tenant_id,document_id,user_id,permission) VALUES($1,$2,$3,'read')`, []any{other, foreignDoc, user}},
		{`INSERT INTO document_acl(tenant_id,document_id,role_id,permission) VALUES($1,$2,$3,'read')`, []any{other, foreignDoc, role}},
	} {
		_, err := pool.Exec(ctx, tc.query, tc.args...)
		var constraintError *pgconn.PgError
		if !errors.As(err, &constraintError) || constraintError.Code != "23503" {
			t.Fatalf("expected cross-tenant foreign-key rejection, got %v", err)
		}
	}
}
