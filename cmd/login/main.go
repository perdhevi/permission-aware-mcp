// Command login obtains a Keycloak access token using a browser and PKCE.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/config"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	if c.AuthMode != "oidc" {
		return errors.New("login requires AUTH_MODE=oidc")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	metadata, err := auth.GetAuthServerMetadata(ctx, c.Issuer, client)
	if err != nil || metadata == nil || metadata.Issuer != c.Issuer || !authn.ValidProviderURL(metadata.AuthorizationEndpoint) || !authn.ValidProviderURL(metadata.TokenEndpoint) {
		return errors.New("authorization server discovery failed")
	}
	if !slices.Contains(metadata.CodeChallengeMethodsSupported, "S256") {
		return errors.New("authorization server must support S256 PKCE")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:8765")
	if err != nil {
		return errors.New("cannot listen on 127.0.0.1:8765; close other login commands")
	}
	defer listener.Close()
	oauth := oauth2.Config{ClientID: "permission-mcp-cli", RedirectURL: "http://127.0.0.1:8765/callback", Scopes: []string{c.RequiredScope}, Endpoint: oauth2.Endpoint{AuthURL: metadata.AuthorizationEndpoint, TokenURL: metadata.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
	var random [32]byte
	if _, err = rand.Read(random[:]); err != nil {
		return errors.New("cannot create OAuth state")
	}
	state := base64.RawURLEncoding.EncodeToString(random[:])
	verifier := oauth2.GenerateVerifier()
	codes := make(chan string, 1)
	var received atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state || r.URL.Query().Get("code") == "" || r.URL.Query().Get("error") != "" {
			http.Error(w, "invalid login callback", 400)
			return
		}
		if metadata.AuthorizationResponseIssParameterSupported && r.URL.Query().Get("iss") != c.Issuer {
			http.Error(w, "invalid response issuer", 400)
			return
		}
		if !received.CompareAndSwap(false, true) {
			http.Error(w, "callback already received", 409)
			return
		}
		select {
		case codes <- r.URL.Query().Get("code"):
			fmt.Fprintln(w, "Login received. Return to your terminal.")
		default:
			http.Error(w, "callback already received", 409)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	fmt.Fprintln(os.Stderr, "Open this URL in your browser and sign in:")
	fmt.Fprintln(os.Stderr, oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("resource", c.ResourceURL)))
	var code string
	select {
	case code = <-codes:
	case <-ctx.Done():
		return errors.New("login timed out")
	}
	token, err := oauth.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), code, oauth2.VerifierOption(verifier), oauth2.SetAuthURLParam("resource", c.ResourceURL))
	if err != nil {
		return errors.New("code exchange failed")
	}
	check, err := authn.NewOIDC(ctx, c.Issuer, c.Audience, c.RequiredScope)
	if err != nil {
		return err
	}
	if _, err = check.Verify(ctx, token.AccessToken); err != nil {
		return errors.New("authorization server returned an invalid access token")
	}
	fmt.Println(token.AccessToken)
	return nil
}
