package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/rag"
	"permission-aware-mcp/internal/repository/postgres"
)

// Exercise the real seeded Keycloak login form, code exchange and PKCE. This is
// a deterministic development fixture driver, not a general browser automation.
func keycloakToken(t *testing.T, issuer, username string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	metadata, err := auth.GetAuthServerMetadata(ctx, issuer, client)
	if err != nil || metadata == nil {
		t.Fatal("Keycloak discovery unavailable")
	}
	config := oauth2.Config{ClientID: "permission-mcp-cli", RedirectURL: "http://127.0.0.1:8765/callback", Scopes: []string{"knowledge:read"}, Endpoint: oauth2.Endpoint{AuthURL: metadata.AuthorizationEndpoint, TokenURL: metadata.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
	verifier := oauth2.GenerateVerifier()
	resp, err := client.Get(config.AuthCodeURL("integration-state", oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", "http://127.0.0.1:8080/mcp")))
	if err != nil {
		t.Fatal("Keycloak authorization request failed")
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	action := regexp.MustCompile(`action="([^"]+)"`).FindSubmatch(body)
	if resp.StatusCode != 200 || len(action) != 2 {
		t.Fatalf("Keycloak login form unavailable (HTTP %d)", resp.StatusCode)
	}
	target, err := url.Parse(html.UnescapeString(string(action[1])))
	base, _ := url.Parse(issuer)
	if err != nil || target.Host != base.Host {
		t.Fatal("unexpected Keycloak login target")
	}
	resp, err = client.PostForm(target.String(), url.Values{"username": {username}, "password": {"local-demo-only"}, "credentialId": {""}})
	if err != nil {
		t.Fatal("Keycloak login failed")
	}
	resp.Body.Close()
	callback, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != 302 || callback.Host != "127.0.0.1:8765" || callback.Query().Get("state") != "integration-state" || callback.Query().Get("code") == "" {
		t.Fatalf("Keycloak did not return authorization code (HTTP %d)", resp.StatusCode)
	}
	token, err := config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), callback.Query().Get("code"), oauth2.VerifierOption(verifier), oauth2.SetAuthURLParam("resource", "http://127.0.0.1:8080/mcp"))
	if err != nil {
		t.Fatal("Keycloak PKCE exchange failed")
	}
	return token.AccessToken
}

func TestKeycloakIntegration(t *testing.T) {
	issuer := os.Getenv("TEST_OIDC_ISSUER")
	database := os.Getenv("TEST_DATABASE_URL")
	if issuer == "" || database == "" {
		t.Skip("set TEST_OIDC_ISSUER and TEST_DATABASE_URL with local Keycloak/SQL fixtures")
	}
	pool, err := pgxpool.New(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	verifier, err := authn.NewOIDC(t.Context(), issuer, "http://127.0.0.1:8080/mcp", "knowledge:read")
	if err != nil {
		t.Fatal(err)
	}
	documents := postgres.NewDocuments(pool)
	// Use an isolated in-memory audit store here; persistent writes and failure
	// behavior are covered by TestAuditedInjectionIntegration against the same DB.
	store := &memoryAudit{}
	srv := httptest.NewServer(New(verifier, authn.NewResolver(postgres.NewUsers(pool)), pool, "knowledge:read", authz.New(documents), rag.NewRetriever(documents, rag.HashEmbedder{}), slog.New(slog.NewTextHandler(io.Discard, nil)), store, OAuthDiscovery{Resource: "http://127.0.0.1:8080/mcp", Issuer: issuer}))
	defer srv.Close()
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		for i, username := range []string{"alice", "bob", "carol", "dave"} {
			t.Run(version+"/"+username, func(t *testing.T) {
				raw := keycloakToken(t, issuer, username)
				identity, err := verifier.Verify(t.Context(), raw)
				subjects := []string{"10000000-0000-0000-0000-000000000001", "10000000-0000-0000-0000-000000000002", "10000000-0000-0000-0000-000000000003", "10000000-0000-0000-0000-000000000004"}
				if err != nil || identity.Subject != subjects[i] {
					parts := strings.Split(raw, ".")
					var claims struct {
						Subject  string `json:"sub"`
						Audience any    `json:"aud"`
						Type     string `json:"typ"`
						Scope    string `json:"scope"`
					}
					if len(parts) == 3 {
						data, _ := base64.RawURLEncoding.DecodeString(parts[1])
						_ = json.Unmarshal(data, &claims)
					}
					t.Fatalf("Keycloak identity/profile mismatch: subject=%q audience=%v type=%q scope=%q verify=%v", claims.Subject, claims.Audience, claims.Type, claims.Scope, err)
				}
				client := mcp.NewClient(&mcp.Implementation{Name: "oidc-security-test", Version: "1"}, nil)
				session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: raw}}}, &mcp.ClientSessionOptions{ProtocolVersion: version})
				if err != nil {
					t.Fatal("OIDC MCP connection failed")
				}
				defer session.Close()
				result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
				if err != nil || result.IsError {
					t.Fatal("OIDC whoami failed")
				}
				encoded, _ := json.Marshal(result.StructuredContent)
				if !strings.Contains(string(encoded), subjects[i]) || !strings.Contains(string(encoded), "acme") {
					t.Fatal("wrong database principal")
				}
				result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_accessible_sources", Arguments: map[string]any{"limit": 20}})
				if err != nil || result.IsError {
					t.Fatal("OIDC listing failed")
				}
				encoded, _ = json.Marshal(result.StructuredContent)
				var listing struct {
					Sources []struct {
						ID    string `json:"document_id"`
						Title string `json:"title"`
					} `json:"sources"`
				}
				if json.Unmarshal(encoded, &listing) != nil || len(listing.Sources) != []int{2, 2, 4, 1}[i] {
					t.Fatalf("wrong permissions: %s", encoded)
				}
				if username == "alice" {
					var salary string
					if err := pool.QueryRow(t.Context(), "SELECT id::text FROM documents WHERE source='salary-guidelines' AND tenant_id=(SELECT id FROM tenants WHERE slug='acme')").Scan(&salary); err != nil {
						t.Fatal(err)
					}
					result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "get_document", Arguments: map[string]any{"document_id": salary}})
					if err != nil || !result.IsError {
						t.Fatal("OIDC Alice accessed salary")
					}
				}
				result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{"role": "executive"}})
				if err == nil && !result.IsError {
					t.Fatal("OIDC spoofing accepted")
				}
			})
		}
	}
}
