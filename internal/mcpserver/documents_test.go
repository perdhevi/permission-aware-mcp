package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/rag"
	"permission-aware-mcp/internal/repository"
)

const docID = "00000000-0000-0000-0000-000000000003"

type resourceVerifier struct{}

func (resourceVerifier) Verify(_ context.Context, raw string) (*authn.Identity, error) {
	return &authn.Identity{Issuer: "test", Subject: raw, Scopes: []string{"knowledge:read"}, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type resourceUsers struct{}

func (resourceUsers) FindByExternalIdentity(_ context.Context, _, subject string) (*domain.User, error) {
	id := "00000000-0000-0000-0000-000000000001"
	if subject == "bob" {
		id = "00000000-0000-0000-0000-000000000004"
	}
	return &domain.User{ID: id, Subject: subject, TenantID: "00000000-0000-0000-0000-000000000002", TenantSlug: "test", Active: true, Roles: []string{"employee"}}, nil
}

type resourceDocuments struct{}

func (resourceDocuments) ListReadable(_ context.Context, tenant, user string, limit int) ([]domain.Source, error) {
	if tenant != "00000000-0000-0000-0000-000000000002" {
		return nil, errors.New("wrong tenant")
	}
	if strings.HasSuffix(user, "4") {
		return []domain.Source{}, nil
	}
	return []domain.Source{{DocumentID: docID, Title: "Visible"}}, nil
}
func (resourceDocuments) GetReadable(_ context.Context, tenant, user, id string, max int) (*domain.Document, error) {
	if id == "00000000-0000-0000-0000-000000000099" {
		return nil, errors.New("secret database diagnostic")
	}
	if strings.HasSuffix(user, "4") || id != docID {
		return nil, repository.ErrNotFound
	}
	text := []rune("Visible content")
	truncated := len(text) > max
	if truncated {
		text = text[:max]
	}
	return &domain.Document{DocumentID: id, Title: "Visible", Content: string(text), Truncated: truncated}, nil
}
func (resourceDocuments) SearchReadable(_ context.Context, tenant, user, vector, model string, dimensions, limit int) ([]domain.SearchResult, error) {
	if tenant != "00000000-0000-0000-0000-000000000002" {
		return nil, errors.New("wrong tenant")
	}
	if strings.HasSuffix(user, "4") {
		return nil, nil
	}
	return []domain.SearchResult{{ChunkID: docID, DocumentID: docID, Title: "Visible", Content: "Visible content", Distance: 0.25}}, nil
}
func TestHTTPDocumentBoundary(t *testing.T) {
	srv := httptest.NewServer(New(resourceVerifier{}, authn.NewResolver(resourceUsers{}), ping{}, "knowledge:read", authz.New(resourceDocuments{}), rag.NewRetriever(resourceDocuments{}, rag.HashEmbedder{}), slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer srv.Close()
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		for _, subject := range []string{"alice", "bob", "alice"} {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			client := mcp.NewClient(&mcp.Implementation{Name: "resource-test", Version: "1"}, nil)
			session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: subject}}}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			call := func(name string, args map[string]any) *mcp.CallToolResult {
				t.Helper()
				r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			result := call("list_accessible_sources", map[string]any{})
			if result.IsError {
				t.Fatalf("list: %+v", result)
			}
			b, _ := json.Marshal(result.StructuredContent)
			var out struct {
				Sources []domain.Source `json:"sources"`
			}
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			want := 1
			if subject == "bob" {
				want = 0
			}
			if len(out.Sources) != want {
				t.Fatalf("%s: %s", subject, b)
			}
			search := call("search_knowledge", map[string]any{"query": "deployment"})
			b, _ = json.Marshal(search.StructuredContent)
			var found struct {
				Results []domain.SearchResult `json:"results"`
			}
			if err := json.Unmarshal(b, &found); err != nil || search.IsError || len(found.Results) != want || found.Results == nil {
				t.Fatalf("%s search: %s %v", subject, b, err)
			}
			result = call("get_document", map[string]any{"document_id": docID, "max_chars": 3})
			if result.IsError != (subject == "bob") {
				t.Fatalf("%s direct result: %+v", subject, result)
			}
			if subject == "alice" {
				b, _ := json.Marshal(result.StructuredContent)
				var d domain.Document
				if err := json.Unmarshal(b, &d); err != nil {
					t.Fatal(err)
				}
				if d.Content != "Vis" || !d.Truncated {
					t.Fatalf("%+v", d)
				}
			}
			for _, tc := range []struct {
				name string
				args map[string]any
			}{
				{"search_knowledge", map[string]any{}},
				{"search_knowledge", map[string]any{"query": ""}},
				{"search_knowledge", map[string]any{"query": "  "}},
				{"search_knowledge", map[string]any{"query": strings.Repeat("界", 4001)}},
				{"search_knowledge", map[string]any{"query": "salary", "role": "executive"}},
				{"search_knowledge", map[string]any{"query": "salary", "user_id": "carol"}},
				{"search_knowledge", map[string]any{"query": "salary", "tenant_id": "other"}},
				{"search_knowledge", map[string]any{"query": "salary", "limit": 0}},
				{"search_knowledge", map[string]any{"query": "salary", "limit": 21}},
				{"search_knowledge", map[string]any{"query": "salary", "limit": 1.5}},
				{"list_accessible_sources", map[string]any{"role": "executive"}},
				{"list_accessible_sources", map[string]any{"limit": 0}},
				{"list_accessible_sources", map[string]any{"limit": 101}},
				{"list_accessible_sources", map[string]any{"limit": 1.5}},
				{"get_document", map[string]any{}},
				{"get_document", map[string]any{"document_id": docID, "user_id": "carol"}},
				{"get_document", map[string]any{"document_id": docID, "tenant_id": "other"}},
				{"get_document", map[string]any{"document_id": docID, "max_chars": 0}},
				{"get_document", map[string]any{"document_id": docID, "max_chars": 50001}},
			} {
				r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
				if err == nil && !r.IsError {
					t.Fatalf("accepted invalid arguments: %+v", tc)
				}
			}
			for _, id := range []string{"invalid", "00000000-0000-0000-0000-000000000000"} {
				r := call("get_document", map[string]any{"document_id": id})
				b, _ := json.Marshal(r)
				if !r.IsError || !strings.Contains(string(b), "resource not found") {
					t.Fatalf("denial leaked: %s", b)
				}
			}
			r := call("get_document", map[string]any{"document_id": "00000000-0000-0000-0000-000000000099"})
			b, _ = json.Marshal(r)
			if !r.IsError || !strings.Contains(string(b), "resource service unavailable") || strings.Contains(string(b), "secret database") {
				t.Fatalf("error leaked: %s", b)
			}
			session.Close()
			cancel()
		}
	}
}
