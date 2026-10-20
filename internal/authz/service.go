package authz

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/repository"
)

var ErrInvalidInput = errors.New("invalid resource arguments")

// Service owns authorization orchestration. The repository applies the same policy
// atomically with each read; a separate check followed by an unguarded read is unsafe.
type Service struct{ documents repository.DocumentRepository }

func New(documents repository.DocumentRepository) *Service { return &Service{documents: documents} }
func validPrincipal(p *authn.Principal) bool {
	return p != nil && validUUID(p.UserID) && validUUID(p.TenantID)
}
func validUUID(s string) bool { var id pgtype.UUID; return id.Scan(s) == nil && id.Valid }
func (s *Service) ListAccessibleSources(ctx context.Context, p *authn.Principal, limit int) ([]domain.Source, error) {
	if !validPrincipal(p) {
		return nil, authn.ErrUnauthenticated
	}
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidInput
	}
	return s.documents.ListReadable(ctx, p.TenantID, p.UserID, limit)
}
func (s *Service) GetDocument(ctx context.Context, p *authn.Principal, id string, maxChars int) (*domain.Document, error) {
	if !validPrincipal(p) {
		return nil, authn.ErrUnauthenticated
	}
	if !validUUID(id) {
		return nil, repository.ErrNotFound
	}
	if maxChars < 1 || maxChars > 50000 {
		return nil, ErrInvalidInput
	}
	return s.documents.GetReadable(ctx, p.TenantID, p.UserID, id, maxChars)
}
