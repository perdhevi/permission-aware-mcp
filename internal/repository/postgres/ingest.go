package postgres

import (
	"context"
	"fmt"

	"permission-aware-mcp/internal/rag"
)

// Ingest is a trusted database administration operation, never an MCP tool.
// A document row lock serializes replacements and prevents concurrent edits.
func (r *Documents) Ingest(ctx context.Context, tenant string, embedder rag.Embedder) (int, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.id::text FROM documents d JOIN tenants t ON t.id=d.tenant_id WHERE t.slug=$1 ORDER BY d.id`, tenant)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("tenant has no documents")
	}
	for i, id := range ids {
		if err := r.ingestDocument(ctx, id, embedder); err != nil {
			return i, err
		}
	}
	return len(ids), nil
}
func (r *Documents) ingestDocument(ctx context.Context, id string, e rag.Embedder) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var tenant, content, hash string
	if err := tx.QueryRow(ctx, `SELECT tenant_id::text,content,md5(content) FROM documents WHERE id=$1 FOR UPDATE`, id).Scan(&tenant, &content, &hash); err != nil {
		return err
	}
	parts := rag.Chunk(content)
	vectors, err := e.EmbedDocuments(ctx, parts)
	if err != nil {
		return err
	}
	if len(vectors) != len(parts) {
		return fmt.Errorf("embedding count mismatch")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE document_id=$1`, id); err != nil {
		return err
	}
	for i, part := range parts {
		vector, err := rag.VectorLiteral(vectors[i], e.Dimensions())
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chunks(tenant_id,document_id,ordinal,content,embedding,embedding_model,embedding_dimensions,document_hash) VALUES($1,$2,$3,$4,$5::vector,$6,$7,$8)`, tenant, id, i, part, vector, e.Model(), e.Dimensions(), hash); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
