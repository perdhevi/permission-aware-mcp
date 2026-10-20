package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"permission-aware-mcp/internal/audit"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/mcpserver/tools"
	"permission-aware-mcp/internal/rag"
)

type Pinger interface{ Ping(context.Context) error }

func New(verifier authn.TokenVerifier, resolver *authn.Resolver, db Pinger, scope string, authorization *authz.Service, retrieval *rag.Retriever, logger *slog.Logger, store audit.Store) http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "permission-aware-mcp", Version: "0.1.0"}, nil)
	tools.RegisterWhoami(s)
	tools.RegisterDocuments(s, authorization)
	if retrieval != nil {
		tools.RegisterSearch(s, retrieval)
	}
	s.AddReceivingMiddleware(auditMiddleware(store, logger))
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	verify := func(ctx context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		id, err := verifier.Verify(ctx, raw)
		if err != nil {
			return nil, auth.ErrInvalidToken
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		p, err := resolver.Resolve(lookupCtx, id)
		if errors.Is(err, authn.ErrUnauthenticated) {
			return nil, auth.ErrInvalidToken
		}
		if err != nil {
			logger.ErrorContext(ctx, "identity lookup failed", "error", "repository unavailable")
			return nil, errors.New("identity service unavailable")
		}
		return &auth.TokenInfo{UserID: p.UserID, Scopes: p.Scopes, Expiration: id.ExpiresAt, Extra: map[string]any{tools.PrincipalKey: p}}, nil
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.RequireBearerToken(verify, &auth.RequireBearerTokenOptions{Scopes: []string{scope}})(transport))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var b [16]byte
		_, _ = rand.Read(b[:])
		requestID := hex.EncodeToString(b[:])
		w.Header().Set("X-Request-ID", requestID)
		r.Header.Set("X-Request-ID", requestID)
		// Never log headers, request bodies, query strings or authentication errors.
		mux.ServeHTTP(w, r)
		logger.InfoContext(r.Context(), "http request", "request_id", requestID, "method", r.Method, "duration", time.Since(start))
	})
}
