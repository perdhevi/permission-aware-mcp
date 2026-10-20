package rag

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/authz"
	"permission-aware-mcp/internal/domain"
)

type SearchRepository interface {
	SearchReadable(context.Context, string, string, string, string, int, int) ([]domain.SearchResult, error)
}
type Retriever struct {
	repository SearchRepository
	embedder   Embedder
}

func NewRetriever(r SearchRepository, e Embedder) *Retriever { return &Retriever{r, e} }
func (r *Retriever) Search(ctx context.Context, p *authn.Principal, query string, limit int) ([]domain.SearchResult, error) {
	valid := func(s string) bool { var id pgtype.UUID; return id.Scan(s) == nil && id.Valid }
	if p == nil || !valid(p.UserID) || !valid(p.TenantID) {
		return nil, authn.ErrUnauthenticated
	}
	if !utf8.ValidString(query) || strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 4000 || limit < 1 || limit > 20 {
		return nil, authz.ErrInvalidInput
	}
	vector, err := r.embedder.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	literal, err := VectorLiteral(vector, r.embedder.Dimensions())
	if err != nil {
		return nil, err
	}
	return r.repository.SearchReadable(ctx, p.TenantID, p.UserID, literal, r.embedder.Model(), r.embedder.Dimensions(), limit)
}
