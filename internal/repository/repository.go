package repository

import (
	"context"
	"errors"

	"permission-aware-mcp/internal/domain"
)

var ErrNotFound = errors.New("user not found")

type UserRepository interface {
	FindByExternalIdentity(context.Context, string, string) (*domain.User, error)
}
