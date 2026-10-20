package mcpserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthDiscovery(t *testing.T) {
	h := New(nil, nil, ping{}, "knowledge:read", nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), &memoryAudit{}, OAuthDiscovery{Resource: "http://127.0.0.1:8080/mcp", Issuer: "http://127.0.0.1:8081/realms/permission-mcp"})
	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "attacker.example"
		r.Header.Set("X-Forwarded-Host", "attacker.example")
		h.ServeHTTP(w, r)
		var got struct {
			Resource string   `json:"resource"`
			Servers  []string `json:"authorization_servers"`
			Scopes   []string `json:"scopes_supported"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got.Resource != "http://127.0.0.1:8080/mcp" || len(got.Servers) != 1 || got.Servers[0] != "http://127.0.0.1:8081/realms/permission-mcp" || len(got.Scopes) != 1 || got.Scopes[0] != "knowledge:read" {
			t.Fatalf("metadata: %s", w.Body.String())
		}
		for method, want := range map[string]int{"OPTIONS": 204, "POST": 405} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != want {
				t.Fatalf("%s: %d", method, w.Code)
			}
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/mcp", nil))
	if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), `resource_metadata="http://127.0.0.1:8080/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("challenge: %d %v", w.Code, w.Header())
	}
}
