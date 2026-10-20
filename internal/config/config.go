package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"permission-aware-mcp/internal/authn"
)

type Config struct{ HTTPAddr, DatabaseURL, AuthMode, Issuer, Audience, PublicKeyFile, RequiredScope, ResourceURL string }

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return fallback
}
func Load() (Config, error) {
	c := Config{
		HTTPAddr:    env("HTTP_ADDR", "127.0.0.1:8080"),
		ResourceURL: env("RESOURCE_URL", "http://127.0.0.1:8080/mcp"),
		DatabaseURL: env("DATABASE_URL", "postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable"),
		AuthMode:    env("AUTH_MODE", "dev"), Issuer: env("AUTH_ISSUER", "http://dev.local"),
		Audience: env("AUTH_AUDIENCE", "permission-aware-mcp"), PublicKeyFile: env("DEV_JWT_PUBLIC_KEY_FILE", ".local/keys/public.pem"), RequiredScope: env("REQUIRED_SCOPE", "knowledge:read"),
	}
	if env("APP_ENV", "development") != "development" {
		return Config{}, fmt.Errorf("only APP_ENV=development is supported; deployment hardening remains deferred")
	}
	if c.AuthMode != "dev" && c.AuthMode != "oidc" {
		return Config{}, fmt.Errorf("unsupported AUTH_MODE %q", c.AuthMode)
	}
	for k, v := range map[string]string{"HTTP_ADDR": c.HTTPAddr, "DATABASE_URL": c.DatabaseURL, "AUTH_ISSUER": c.Issuer, "AUTH_AUDIENCE": c.Audience, "REQUIRED_SCOPE": c.RequiredScope} {
		if v == "" {
			return Config{}, fmt.Errorf("%s is required", k)
		}
	}
	if len(strings.Fields(c.RequiredScope)) != 1 {
		return Config{}, fmt.Errorf("REQUIRED_SCOPE must be one scope")
	}
	if c.AuthMode == "dev" && c.PublicKeyFile == "" {
		return Config{}, fmt.Errorf("DEV_JWT_PUBLIC_KEY_FILE is required")
	}
	if c.AuthMode == "oidc" && (!authn.ValidProviderURL(c.Issuer) || !authn.ValidProviderURL(c.ResourceURL) || !strings.HasSuffix(c.ResourceURL, "/mcp") || c.Audience != c.ResourceURL) {
		return Config{}, fmt.Errorf("OIDC requires secure issuer and RESOURCE_URL ending in /mcp, with AUTH_AUDIENCE equal to RESOURCE_URL")
	}
	if c.AuthMode == "oidc" {
		u, _ := url.Parse(c.ResourceURL)
		if u.Path != "/mcp" || u.RawPath != "" {
			return Config{}, fmt.Errorf("RESOURCE_URL path must be /mcp")
		}
	}
	return c, nil
}
