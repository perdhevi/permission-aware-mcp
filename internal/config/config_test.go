package config

import "testing"

func TestFailClosedConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"APP_ENV", "production"}, {"AUTH_MODE", "oidc"}, {"AUTH_ISSUER", ""}, {"AUTH_AUDIENCE", ""}, {"DATABASE_URL", ""}, {"REQUIRED_SCOPE", ""}, {"REQUIRED_SCOPE", "knowledge:read knowledge:write"}} {
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
