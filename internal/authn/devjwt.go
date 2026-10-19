package authn

import (
	"context"
	"crypto/rsa"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	jwt.RegisteredClaims
	Scope string `json:"scope"`
}
type DevJWT struct {
	key                     *rsa.PublicKey
	issuer, audience, scope string
}

func NewDevJWT(key *rsa.PublicKey, issuer, audience, scope string) (*DevJWT, error) {
	if key == nil || key.N.BitLen() < 2048 || issuer == "" || audience == "" || scope == "" {
		return nil, errors.New("invalid JWT verifier configuration")
	}
	return &DevJWT{key: key, issuer: issuer, audience: audience, scope: scope}, nil
}
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return jwt.ParseRSAPublicKeyFromPEM(b)
}
func (v *DevJWT) Verify(ctx context.Context, raw string) (*Identity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var claims Claims
	_, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return v.key, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer(v.issuer), jwt.WithAudience(v.audience), jwt.WithExpirationRequired())
	// Deliberately discard parser errors: they can include untrusted claim data.
	if err != nil || strings.TrimSpace(claims.Subject) == "" {
		return nil, ErrUnauthenticated
	}
	scopes := strings.Fields(claims.Scope)
	if !slices.Contains(scopes, v.scope) {
		return nil, ErrUnauthenticated
	}
	return &Identity{Issuer: claims.Issuer, Subject: claims.Subject, Scopes: scopes, ExpiresAt: claims.ExpiresAt.Time}, nil
}
