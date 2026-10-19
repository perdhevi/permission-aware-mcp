package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct{ HTTPAddr, DatabaseURL, AuthMode, Issuer, Audience, PublicKeyFile, RequiredScope string }

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(v)
	}
	return fallback
}
func Load() (Config, error) {
	c := Config{
		HTTPAddr:    env("HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable"),
		AuthMode:    env("AUTH_MODE", "dev"), Issuer: env("AUTH_ISSUER", "http://dev.local"),
		Audience: env("AUTH_AUDIENCE", "permission-aware-mcp"), PublicKeyFile: env("DEV_JWT_PUBLIC_KEY_FILE", ".local/keys/public.pem"), RequiredScope: env("REQUIRED_SCOPE", "knowledge:read"),
	}
	if env("APP_ENV", "development") != "development" {
		return Config{}, fmt.Errorf("only APP_ENV=development is supported; production authentication is not implemented")
	}
	if c.AuthMode != "dev" {
		return Config{}, fmt.Errorf("unsupported AUTH_MODE %q", c.AuthMode)
	}
	for k, v := range map[string]string{"HTTP_ADDR": c.HTTPAddr, "DATABASE_URL": c.DatabaseURL, "AUTH_ISSUER": c.Issuer, "AUTH_AUDIENCE": c.Audience, "DEV_JWT_PUBLIC_KEY_FILE": c.PublicKeyFile, "REQUIRED_SCOPE": c.RequiredScope} {
		if v == "" {
			return Config{}, fmt.Errorf("%s is required", k)
		}
	}
	if len(strings.Fields(c.RequiredScope)) != 1 {
		return Config{}, fmt.Errorf("REQUIRED_SCOPE must be one scope")
	}
	return c, nil
}
