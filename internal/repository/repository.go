package repository

import (
	"context"
	"errors"

	"permission-aware-mcp/internal/domain"
)

var ErrNotFound = errors.New("resource not found")

type UserRepository interface {
	FindByExternalIdentity(context.Context, string, string) (*domain.User, error)
}

// DocumentRepository must authorize inside each query before returning any data.
type DocumentRepository interface {
	ListReadable(context.Context, string, string, int) ([]domain.Source, error)
	GetReadable(context.Context, string, string, string, int) (*domain.Document, error)
}
