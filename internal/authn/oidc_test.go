package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestOIDCVerification(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	var mu sync.Mutex
	active, kid := key, "first"
	var issuer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			mu.Lock()
			defer mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid, "n": base64.RawURLEncoding.EncodeToString(active.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(active.E)).Bytes())}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	issuer = srv.URL
	v, err := NewOIDC(t.Context(), issuer, "resource", "knowledge:read")
	if err != nil {
		t.Fatal(err)
	}
	mint := func(change func(jwt.MapClaims), signing *rsa.PrivateKey, id string, alg jwt.SigningMethod) string {
		claims := jwt.MapClaims{"iss": issuer, "aud": "resource", "sub": "alice", "exp": time.Now().Add(time.Hour).Unix(), "scope": "knowledge:read", "typ": "Bearer", "roles": []string{"executive"}, "tenant_id": "attacker"}
		change(claims)
		tok := jwt.NewWithClaims(alg, claims)
		tok.Header["kid"] = id
		var secret any = signing
		if alg == jwt.SigningMethodHS256 {
			secret = []byte("not-the-public-key")
		}
		raw, err := tok.SignedString(secret)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	unchanged := func(jwt.MapClaims) {}
	good := mint(unchanged, key, "first", jwt.SigningMethodRS256)
	identity, err := v.Verify(t.Context(), good)
	if err != nil || identity.Subject != "alice" || identity.Issuer != issuer {
		t.Fatalf("valid token: %+v %v", identity, err)
	}
	for _, tc := range []struct {
		name   string
		change func(jwt.MapClaims)
	}{
		{"issuer", func(c jwt.MapClaims) { c["iss"] = "https://wrong.example" }},
		{"audience", func(c jwt.MapClaims) { c["aud"] = "other" }},
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"missing expiry", func(c jwt.MapClaims) { delete(c, "exp") }},
		{"future", func(c jwt.MapClaims) { c["nbf"] = time.Now().Add(time.Hour).Unix() }},
		{"scope", func(c jwt.MapClaims) { c["scope"] = "other" }},
		{"subject", func(c jwt.MapClaims) { c["sub"] = " " }},
		{"ID token", func(c jwt.MapClaims) { c["typ"] = "ID" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := v.Verify(t.Context(), mint(tc.change, key, "first", jwt.SigningMethodRS256)); err != ErrUnauthenticated {
				t.Fatalf("accepted invalid token: %v", err)
			}
		})
	}
	for _, raw := range []string{"malformed", mint(unchanged, other, "first", jwt.SigningMethodRS256), mint(unchanged, key, "first", jwt.SigningMethodHS256), mint(unchanged, key, "unknown", jwt.SigningMethodRS256)} {
		if _, err := v.Verify(t.Context(), raw); err != ErrUnauthenticated {
			t.Fatal("invalid signature/algorithm/kid accepted")
		}
	}
	mu.Lock()
	active = other
	kid = "rotated"
	mu.Unlock()
	if _, err := v.Verify(t.Context(), mint(unchanged, other, "rotated", jwt.SigningMethodRS256)); err != nil {
		t.Fatalf("rotation: %v", err)
	}
	srv.Close()
	if _, err := v.Verify(t.Context(), mint(unchanged, other, "rotated", jwt.SigningMethodRS256)); err != nil {
		t.Fatalf("cached key: %v", err)
	}
	if _, err := v.Verify(t.Context(), mint(unchanged, key, "uncached", jwt.SigningMethodRS256)); err != ErrUnauthenticated {
		t.Fatal("outage accepted unknown key")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := v.Verify(ctx, good); err != ErrUnauthenticated {
		t.Fatal("canceled verification accepted")
	}
}

func TestOIDCURLPolicy(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/?query=1", "https://example.com/#fragment", "file:///key", ""} {
		if ValidProviderURL(raw) {
			t.Fatalf("unsafe URL %s", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:8081/realm", "http://localhost/realm", "https://idp.example/realm"} {
		if !ValidProviderURL(raw) {
			t.Fatalf("valid URL %s", raw)
		}
	}
	if _, err := NewOIDC(t.Context(), "http://example.com", "resource", "scope"); err == nil {
		t.Fatal("unsafe issuer accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"issuer": "https://wrong.example", "jwks_uri": "https://wrong.example/keys"})
	}))
	defer server.Close()
	if _, err := NewOIDC(t.Context(), server.URL, "resource", "scope"); err == nil {
		t.Fatal("mismatched discovery issuer accepted")
	}
}
