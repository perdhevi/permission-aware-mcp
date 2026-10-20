package config

import "testing"

func TestOIDCConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("AUTH_ISSUER", "http://127.0.0.1:8081/realms/permission-mcp")
	t.Setenv("AUTH_AUDIENCE", "http://127.0.0.1:8080/mcp")
	t.Setenv("RESOURCE_URL", "http://127.0.0.1:8080/mcp")
	t.Setenv("DEV_JWT_PUBLIC_KEY_FILE", "")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{{"AUTH_ISSUER", "http://idp.example/realm"}, {"AUTH_AUDIENCE", "another-resource"}, {"RESOURCE_URL", "http://127.0.0.1:8080/other/mcp"}, {"RESOURCE_URL", ""}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid OIDC config accepted")
			}
		})
	}
}

func TestFailClosedConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"APP_ENV", "production"}, {"AUTH_MODE", "unsupported"}, {"AUTH_ISSUER", ""}, {"AUTH_AUDIENCE", ""}, {"DATABASE_URL", ""}, {"REQUIRED_SCOPE", ""}, {"REQUIRED_SCOPE", "knowledge:read knowledge:write"}} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("APP_ENV", "development")
			t.Setenv("AUTH_MODE", "dev")
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
