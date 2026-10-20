package main

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLoginPKCECallback(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var issuer, challenge, raw string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration", "/.well-known/oauth-authorization-server":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"}, "authorization_response_iss_parameter_supported": true})
		case "/keys":
			json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "alg": "RS256", "kid": "test", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			if r.ParseForm() != nil {
				http.Error(w, "bad form", 400)
				return
			}
			digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge || r.Form.Get("code") != "fixture-code" || r.Form.Get("client_id") != "permission-mcp-cli" || r.Form.Get("resource") != "http://127.0.0.1:8080/mcp" || r.Form.Get("redirect_uri") != "http://127.0.0.1:8765/callback" {
				http.Error(w, "invalid exchange", 400)
				return
			}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": issuer, "aud": "http://127.0.0.1:8080/mcp", "sub": "fixture-user", "exp": time.Now().Add(time.Minute).Unix(), "scope": "knowledge:read", "typ": "Bearer"})
			token.Header["kid"] = "test"
			raw, _ = token.SignedString(key)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"access_token": raw, "token_type": "Bearer", "expires_in": 60})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "oidc")
	t.Setenv("AUTH_ISSUER", issuer)
	t.Setenv("AUTH_AUDIENCE", "http://127.0.0.1:8080/mcp")
	t.Setenv("RESOURCE_URL", "http://127.0.0.1:8080/mcp")
	t.Setenv("REQUIRED_SCOPE", "knowledge:read")
	output, writeOutput, _ := os.Pipe()
	instructions, writeInstructions, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writeOutput, writeInstructions
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		output.Close()
		writeOutput.Close()
		instructions.Close()
		writeInstructions.Close()
	}()
	done := make(chan error, 1)
	go func() { err := run(); _ = writeInstructions.Close(); done <- err }()
	scanner := bufio.NewScanner(instructions)
	if !scanner.Scan() || !scanner.Scan() {
		t.Fatalf("login did not emit browser instructions: %v", <-done)
	}
	authorization, err := url.Parse(scanner.Text())
	if err != nil {
		t.Fatal("bad authorization URL")
	}
	query := authorization.Query()
	challenge = query.Get("code_challenge")
	if challenge == "" || query.Get("code_challenge_method") != "S256" || query.Get("resource") != "http://127.0.0.1:8080/mcp" || query.Get("state") == "" {
		t.Fatal("missing OAuth security parameters")
	}
	callback := "http://127.0.0.1:8765/callback"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(callback + "?code=fixture-code&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatal("invalid state accepted")
	}
	resp, err = client.Get(callback + "?" + url.Values{"code": {"fixture-code"}, "state": {query.Get("state")}, "iss": {"https://wrong.example"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatal("wrong authorization response issuer accepted")
	}
	resp, err = client.Get(callback + "?" + url.Values{"code": {"fixture-code"}, "state": {query.Get("state")}, "iss": {issuer}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("valid callback rejected")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("login did not complete")
	}
	writeOutput.Close()
	got, _ := io.ReadAll(output)
	if raw == "" || strings.TrimSpace(string(got)) != raw {
		t.Fatal("stdout must contain only verified access token")
	}
}
