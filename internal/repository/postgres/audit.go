package postgres

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"permission-aware-mcp/internal/audit"
)

type Audit struct{ pool *pgxpool.Pool }

func NewAudit(pool *pgxpool.Pool) *Audit { return &Audit{pool} }

func (a *Audit) Append(ctx context.Context, e audit.Event) error {
	_, err := a.pool.Exec(ctx, `INSERT INTO audit_log
 (timestamp,request_id,user_id,tenant_id,tool,action,resource_id,decision,reason,query_hash,duration_ms,disclosed_document_ids)
 VALUES($1,$2,$3::uuid,$4::uuid,$5,$6,NULLIF($7,'')::uuid,$8,$9,NULLIF($10,''),$11,$12::uuid[])`,
		e.Timestamp, e.RequestID, e.UserID, e.TenantID, e.Tool, e.Action, e.ResourceID, e.Decision, e.Reason, e.QueryHash, e.DurationMS, e.DisclosedDocumentIDs)
	return err
}
