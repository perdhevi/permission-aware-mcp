// Package rag implements permission-filtered retrieval and local ingestion.
package rag

import "context"

// Embedder supplies compatible vectors for queries and documents.
type Embedder interface {
	EmbedQuery(context.Context, string) ([]float32, error)
	EmbedDocuments(context.Context, []string) ([][]float32, error)
	Model() string
	Dimensions() int
}
