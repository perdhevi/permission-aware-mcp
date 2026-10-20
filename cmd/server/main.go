package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/config"
	"permission-aware-mcp/internal/mcpserver"
	"permission-aware-mcp/internal/rag"
	"permission-aware-mcp/internal/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var verifier authn.TokenVerifier
	var discovery []mcpserver.OAuthDiscovery
	if c.AuthMode == "oidc" {
		verifier, err = authn.NewOIDC(ctx, c.Issuer, c.Audience, c.RequiredScope)
		discovery = []mcpserver.OAuthDiscovery{{Resource: c.ResourceURL, Issuer: c.Issuer}}
	} else {
		var keyErr error
		key, keyErr := authn.LoadPublicKey(c.PublicKeyFile)
		if keyErr != nil {
			return errors.New("cannot load development public key; run go run ./cmd/devtoken keygen")
		}
		verifier, err = authn.NewDevJWT(key, c.Issuer, c.Audience, c.RequiredScope)
	}
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, c.DatabaseURL)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer pool.Close()
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(checkCtx)
	cancel()
	if err != nil {
		return errors.New("database connection failed; start docker compose up -d --wait")
	}
	server := &http.Server{
		Addr:              c.HTTPAddr,
		Handler:           mcpserver.New(verifier, authn.NewResolver(postgres.NewUsers(pool)), pool, c.RequiredScope, authz.New(postgres.NewDocuments(pool)), rag.NewRetriever(postgres.NewDocuments(pool), rag.HashEmbedder{}), logger, postgres.NewAudit(pool), discovery...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("server listening", "address", c.HTTPAddr, "protocol", "2026-07-28", "auth_mode", c.AuthMode)
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
