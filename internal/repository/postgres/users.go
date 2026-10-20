package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"permission-aware-mcp/internal/domain"
	"permission-aware-mcp/internal/repository"
)

type Users struct{ pool *pgxpool.Pool }

func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool: pool} }
func (r *Users) FindByExternalIdentity(ctx context.Context, issuer, subject string) (*domain.User, error) {
	var u domain.User
	err := r.pool.QueryRow(ctx, `SELECT u.id::text, u.tenant_id::text, t.slug, u.subject, u.display_name, u.active,
 ARRAY(SELECT r.name FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.tenant_id=ur.tenant_id
 WHERE ur.user_id=u.id AND ur.tenant_id=u.tenant_id ORDER BY r.name)
 FROM users u JOIN tenants t ON t.id = u.tenant_id WHERE u.issuer = $1 AND u.subject = $2`, issuer, subject).
		Scan(&u.ID, &u.TenantID, &u.TenantSlug, &u.Subject, &u.DisplayName, &u.Active, &u.Roles)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return &u, nil
}
