package mcpserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/repository"
)

type users struct{}

func (users) FindByExternalIdentity(_ context.Context, issuer, subject string) (*domain.User, error) {
	if issuer != "http://dev.local" || subject == "unknown" {
		return nil, repository.ErrNotFound
	}
	return &domain.User{ID: subject + "-id", Subject: subject, TenantID: subject + "-tenant", TenantSlug: subject + "-org", Active: subject != "inactive"}, nil
}

type ping struct{ err error }

func (p ping) Ping(context.Context) error { return p.err }

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}
func TestHTTPIdentityBoundary(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := authn.NewDevJWT(&key.PublicKey, "http://dev.local", "permission-aware-mcp", "knowledge:read")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	srv := httptest.NewServer(New(verifier, authn.NewResolver(users{}), ping{}, "knowledge:read", nil, nil, slog.New(slog.NewJSONHandler(&logs, nil)), &memoryAudit{}))
	defer srv.Close()
	mint := func(subject string) string {
		raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, authn.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "http://dev.local", Subject: subject, Audience: jwt.ClaimStrings{"permission-aware-mcp"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, Scope: "knowledge:read"}).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	alice := mint("alice")
	for _, token := range []string{"", "malformed", mint("unknown"), mint("inactive")} {
		req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("want 401, got %d", resp.StatusCode)
		}
	}
	// Reuse the same server across distinct callers and both protocol revisions.
	for _, version := range []string{"2026-07-28", "2025-11-25"} {
		for _, subject := range []string{"alice", "bob", "alice"} {
			token := mint(subject)
			client := mcp.NewClient(&mcp.Implementation{Name: "security-test", Version: "1"}, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: token}}}, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{}})
			if err != nil || result.IsError {
				t.Fatalf("call failed: %+v %v", result, err)
			}
			b, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]any
			if err := json.Unmarshal(b, &object); err != nil {
				t.Fatal(err)
			}
			if object["subject"] != subject || object["user_id"] != subject+"-id" || object["tenant"] != subject+"-org" {
				t.Fatalf("identity leaked across requests: %s", b)
			}
			list, err := session.ListTools(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			schema, _ := json.Marshal(list.Tools[0].InputSchema)
			if !bytes.Contains(schema, []byte(`"additionalProperties":false`)) {
				t.Fatalf("schema allows extra inputs: %s", schema)
			}
			spoof, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "whoami", Arguments: map[string]any{"user_id": "carol", "tenant_id": "evil", "role": "admin"}})
			if err == nil && !spoof.IsError {
				t.Fatal("spoofing arguments should be rejected")
			}
			session.Close()
			cancel()
		}
	}
	if strings.Contains(logs.String(), alice) || strings.Contains(logs.String(), "Bearer ") {
		t.Fatal("token logged")
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", path, resp.StatusCode)
		}
	}
}
func TestReadinessFailure(t *testing.T) {
	h := New(nil, nil, ping{errors.New("offline")}, "knowledge:read", nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &memoryAudit{})
	for _, tc := range []struct {
		path string
		want int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.want {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
	}
}
