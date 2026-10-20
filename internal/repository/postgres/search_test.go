package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/rag"
)

type invalidEmbedder struct{ rag.HashEmbedder }

func (invalidEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, rag.Dimensions)
	}
	return vectors, nil
}

func TestSearchIntegration(t *testing.T) {
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
	repo := NewDocuments(pool)
	h := rag.HashEmbedder{}
	retriever := rag.NewRetriever(repo, h)
	var tenant, slug, user, role, doc, secret, foreign string
	scan := func(q string, dest *string, args ...any) {
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
	scan(`INSERT INTO tenants(slug,name) VALUES('phase3-'||gen_random_uuid(),'Search test') RETURNING id::text`, &tenant)
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, q := range []string{`DELETE FROM documents WHERE tenant_id=$1`, `DELETE FROM users WHERE tenant_id=$1`, `DELETE FROM roles WHERE tenant_id=$1`, `DELETE FROM tenants WHERE id=$1`} {
			if _, err := pool.Exec(cleanup, q, tenant); err != nil {
				t.Error(err)
			}
		}
	}()
	scan(`SELECT slug FROM tenants WHERE id=$1`, &slug, tenant)
	scan(`INSERT INTO users(tenant_id,issuer,subject,display_name) VALUES($1,'phase3',gen_random_uuid()::text,'Test') RETURNING id::text`, &user, tenant)
	scan(`INSERT INTO roles(tenant_id,name) VALUES($1,'engineering') RETURNING id::text`, &role, tenant)
	scan(`INSERT INTO documents(tenant_id,title,source,content,classification) VALUES($1,'Allowed','allowed','deployment automation reliability','internal') RETURNING id::text`, &doc, tenant)
	scan(`INSERT INTO documents(tenant_id,title,source,content,classification) VALUES($1,'Secret title','secret','salary','restricted') RETURNING id::text`, &secret, tenant)
	exec(`INSERT INTO document_acl(tenant_id,document_id,role_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, role)
	exec(`INSERT INTO user_roles(tenant_id,user_id,role_id) VALUES($1,$2,$3)`, tenant, user, role)
	if n, err := repo.Ingest(ctx, slug, h); err != nil || n != 2 {
		t.Fatalf("ingest: %d %v", n, err)
	}
	if n, err := repo.Ingest(ctx, slug, h); err != nil || n != 2 {
		t.Fatalf("repeat ingest: %d %v", n, err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE tenant_id=$1`, tenant).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate chunks: %d %v", count, err)
	}
	p := &authn.Principal{UserID: user, TenantID: tenant, Roles: []string{"executive"}}
	check := func(want int) {
		t.Helper()
		hits, err := retriever.Search(ctx, p, "salary", 1)
		if err != nil || len(hits) != want {
			t.Fatalf("search: %+v %v", hits, err)
		}
		for _, hit := range hits {
			if hit.DocumentID != doc || hit.Title != "Allowed" {
				t.Fatalf("unauthorized hit: %+v", hit)
			}
		}
	}
	check(1) // The forbidden exact match must not consume the top-1 slot.
	if err := repo.ingestDocument(ctx, doc, invalidEmbedder{}); err == nil {
		t.Fatal("accepted invalid provider vector")
	}
	check(1) // Invalid vectors fail after DELETE; the transaction must restore chunks.
	vector, _ := h.EmbedQuery(ctx, "salary")
	literal, _ := rag.VectorLiteral(vector, rag.Dimensions)
	for _, tc := range []struct {
		model      string
		dimensions int
	}{{"other-model", rag.Dimensions}, {rag.Model, 64}} {
		hits, err := repo.SearchReadable(ctx, tenant, user, literal, tc.model, tc.dimensions, 20)
		if err != nil || len(hits) != 0 {
			t.Fatalf("incompatible embeddings: %v %v", hits, err)
		}
	}
	exec(`DELETE FROM user_roles WHERE user_id=$1`, user)
	check(0)
	exec(`INSERT INTO document_acl(tenant_id,document_id,user_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, user)
	check(1)
	exec(`DELETE FROM document_acl WHERE document_id=$1 AND user_id=$2`, doc, user)
	check(0)
	exec(`INSERT INTO document_acl(tenant_id,document_id,user_id,permission) VALUES($1,$2,$3,'read')`, tenant, doc, user)
	exec(`UPDATE users SET active=false WHERE id=$1`, user)
	check(0)
	exec(`UPDATE users SET active=true WHERE id=$1`, user)
	exec(`UPDATE documents SET content='updated content' WHERE id=$1`, doc)
	check(0)
	if _, err := repo.Ingest(ctx, slug, h); err != nil {
		t.Fatal(err)
	}
	check(1)
	// A provider failure preserves the previously committed chunks for this document.
	exec(`UPDATE documents SET content='!!!' WHERE id=$1`, doc)
	if err := repo.ingestDocument(ctx, doc, h); err == nil {
		t.Fatal("expected provider failure")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE document_id=$1`, doc).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed ingestion removed committed chunks", err)
	}
	check(0)
	scan(`SELECT id::text FROM tenants WHERE slug='acme'`, &foreign)
	p.TenantID = foreign
	check(0)
	p.TenantID = tenant
	// Verify the seed permission matrix through real retrieval.
	if _, err := repo.Ingest(ctx, "acme", h); err != nil {
		t.Fatal(err)
	}
	resolver := authn.NewResolver(NewUsers(pool))
	for subject, want := range map[string]int{"alice": 2, "bob": 2, "carol": 4, "dave": 1} {
		caller, err := resolver.Resolve(ctx, &authn.Identity{Issuer: "http://dev.local", Subject: subject})
		if err != nil {
			t.Fatal(err)
		}
		hits, err := retriever.Search(ctx, caller, "salary", 20)
		if err != nil || len(hits) != want {
			t.Fatalf("%s: %+v %v", subject, hits, err)
		}
		for _, hit := range hits {
			if (subject == "alice" || subject == "dave") && hit.Title == "Salary Guidelines" {
				t.Fatal("salary leaked")
			}
			if subject == "bob" && hit.Title == "Engineering Roadmap" {
				t.Fatal("roadmap leaked")
			}
		}
	}
}
