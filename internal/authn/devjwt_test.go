package authn

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestDevJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewDevJWT(&key.PublicKey, "http://dev.local", "permission-aware-mcp", "knowledge:read")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Claims)
		key    *rsa.PrivateKey
		method jwt.SigningMethod
		wantOK bool
	}{
		{name: "valid", wantOK: true},
		{name: "expired", change: func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }},
		{name: "missing expiration", change: func(c *Claims) { c.ExpiresAt = nil }},
		{name: "wrong issuer", change: func(c *Claims) { c.Issuer = "http://evil.local" }},
		{name: "wrong audience", change: func(c *Claims) { c.Audience = jwt.ClaimStrings{"other"} }},
		{name: "invalid signature", key: other},
		{name: "missing scope", change: func(c *Claims) { c.Scope = "" }},
		{name: "scope substring", change: func(c *Claims) { c.Scope = "knowledge:read:admin" }},
		{name: "missing subject", change: func(c *Claims) { c.Subject = "" }},
		{name: "blank subject", change: func(c *Claims) { c.Subject = "  " }},
		{name: "not yet valid", change: func(c *Claims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }},
		{name: "different algorithm", method: jwt.SigningMethodRS512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: "http://dev.local", Subject: "alice", Audience: jwt.ClaimStrings{"permission-aware-mcp"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}, Scope: "knowledge:read"}
			if tc.change != nil {
				tc.change(&c)
			}
			k := tc.key
			if k == nil {
				k = key
			}
			method := tc.method
			if method == nil {
				method = jwt.SigningMethodRS256
			}
			raw, err := jwt.NewWithClaims(method, c).SignedString(k)
			if err != nil {
				t.Fatal(err)
			}
			id, err := v.Verify(context.Background(), raw)
			if tc.wantOK {
				if err != nil || id.Subject != "alice" || id.Issuer != "http://dev.local" {
					t.Fatalf("identity=%+v err=%v", id, err)
				}
			} else if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("expected unauthenticated, got %v", err)
			}
		})
	}
	for _, raw := range []string{"", "nonsense", "a.b.c"} {
		if _, err := v.Verify(context.Background(), raw); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("malformed token accepted")
		}
	}
}
