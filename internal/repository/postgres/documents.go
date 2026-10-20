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

type Documents struct{ pool *pgxpool.Pool }

func NewDocuments(pool *pgxpool.Pool) *Documents { return &Documents{pool: pool} }

// $1 and $2 are always the server-resolved tenant and local user IDs.
// Re-check active membership and roles here, including changes since authentication.
const readable = `d.tenant_id=$1::uuid AND EXISTS (
 SELECT 1 FROM users u WHERE u.id=$2::uuid AND u.tenant_id=d.tenant_id AND u.active
 AND EXISTS (SELECT 1 FROM document_acl a
 WHERE a.tenant_id=d.tenant_id AND a.document_id=d.id AND a.permission='read'
 AND (a.user_id=u.id OR EXISTS (
 SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id AND r.tenant_id=ur.tenant_id
 WHERE ur.tenant_id=u.tenant_id AND ur.user_id=u.id AND ur.role_id=a.role_id))))`

func (r *Documents) ListReadable(ctx context.Context, tenant, user string, limit int) ([]domain.Source, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.id::text,d.title FROM documents d WHERE `+readable+` ORDER BY d.title,d.id LIMIT $3`, tenant, user, limit)
	if err != nil {
		return nil, fmt.Errorf("list readable documents: %w", err)
	}
	defer rows.Close()
	result := []domain.Source{}
	for rows.Next() {
		var s domain.Source
		if err := rows.Scan(&s.DocumentID, &s.Title); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Documents) GetReadable(ctx context.Context, tenant, user, id string, maxChars int) (*domain.Document, error) {
	var d domain.Document
	err := r.pool.QueryRow(ctx, `SELECT d.id::text,d.title,left(d.content,$4),char_length(d.content)>$4 FROM documents d
 WHERE `+readable+` AND d.id=$3::uuid`, tenant, user, id, maxChars).Scan(&d.DocumentID, &d.Title, &d.Content, &d.Truncated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get readable document: %w", err)
	}
	return &d, nil
}
