package postgres

import (
	"context"
	"permission-aware-mcp/internal/domain"
)

func (r *Documents) SearchReadable(ctx context.Context, tenant, user, vector, model string, dimensions, limit int) ([]domain.SearchResult, error) {
	// MATERIALIZED makes the authorization boundary explicit before exact ranking.
	rows, err := r.pool.Query(ctx, `WITH authorized AS MATERIALIZED (
 SELECT c.id,c.document_id,d.title,c.content,c.embedding FROM documents d
 JOIN chunks c ON c.document_id=d.id AND c.tenant_id=d.tenant_id
 WHERE `+readable+` AND c.embedding_model=$4 AND c.embedding_dimensions=$5
 AND c.document_hash=md5(d.content))
 SELECT id::text,document_id::text,title,content,embedding <=> $3::vector AS distance
 FROM authorized ORDER BY distance,document_id,id LIMIT $6`, tenant, user, vector, model, dimensions, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SearchResult{}
	for rows.Next() {
		var hit domain.SearchResult
		if err := rows.Scan(&hit.ChunkID, &hit.DocumentID, &hit.Title, &hit.Content, &hit.Distance); err != nil {
			return nil, err
		}
		out = append(out, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
