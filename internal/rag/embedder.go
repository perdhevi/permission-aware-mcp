// Package rag holds contracts for future permission-filtered retrieval.
package rag

import "context"

// Embedder is a future provider contract; Phase 1 has no implementation.
type Embedder interface {
	EmbedQuery(context.Context, string) ([]float32, error)
	EmbedDocuments(context.Context, []string) ([][]float32, error)
	Model() string
	Dimensions() int
}
