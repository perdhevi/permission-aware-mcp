package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"permission-aware-mcp/internal/config"
	"permission-aware-mcp/internal/rag"
	"permission-aware-mcp/internal/repository/postgres"
)

func main() {
	tenant := flag.String("tenant", "acme", "tenant slug to ingest from existing database documents")
	flag.Parse()
	if err := run(*tenant); err != nil {
		fmt.Fprintln(os.Stderr, "ingestion failed:", err)
		os.Exit(1)
	}
}
func run(tenant string) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, c.DatabaseURL)
	if err != nil {
		return fmt.Errorf("invalid database configuration")
	}
	defer pool.Close()
	count, err := postgres.NewDocuments(pool).Ingest(ctx, tenant, rag.HashEmbedder{})
	if err != nil {
		return fmt.Errorf("database or embedding operation failed after %d documents; verify migration 003 and document text", count)
	}
	fmt.Printf("Ingested %d documents using %s (%d dimensions)\n", count, rag.Model, rag.Dimensions)
	return nil
}
