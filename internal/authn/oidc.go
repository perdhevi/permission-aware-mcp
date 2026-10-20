package authn

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDC accepts Keycloak JWT access tokens. Roles and tenants remain local.
type OIDC struct {
	verifier *oidc.IDTokenVerifier
	scope    string
}

// ValidProviderURL permits cleartext only on literal loopback development hosts.
func ValidProviderURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && slices.Contains([]string{"localhost", "127.0.0.1", "::1"}, u.Hostname())
}

func NewOIDC(ctx context.Context, issuer, audience, scope string) (*OIDC, error) {
	if !ValidProviderURL(issuer) || audience == "" || len(strings.Fields(scope)) != 1 {
		return nil, errors.New("invalid OIDC configuration")
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	ctx = oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	var metadata struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if provider.Claims(&metadata) != nil || !ValidProviderURL(metadata.JWKSURI) {
		return nil, errors.New("invalid OIDC key endpoint")
	}
	if strings.HasPrefix(issuer, "https:") && !strings.HasPrefix(metadata.JWKSURI, "https:") {
		return nil, errors.New("insecure OIDC key endpoint")
	}
	return &OIDC{provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{"RS256"}}), scope}, nil
}

func (v *OIDC) Verify(ctx context.Context, raw string) (*Identity, error) {
	if ctx.Err() != nil {
		return nil, ErrUnauthenticated
	}
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	var claims struct {
		Scope string `json:"scope"`
		Type  string `json:"typ"`
	}
	if token.Claims(&claims) != nil || claims.Type != "Bearer" || strings.TrimSpace(token.Subject) == "" || token.Expiry.IsZero() {
		return nil, ErrUnauthenticated
	}
	scopes := strings.Fields(claims.Scope)
	if !slices.Contains(scopes, v.scope) {
		return nil, ErrUnauthenticated
	}
	return &Identity{Issuer: token.Issuer, Subject: token.Subject, Scopes: scopes, ExpiresAt: token.Expiry}, nil
}
