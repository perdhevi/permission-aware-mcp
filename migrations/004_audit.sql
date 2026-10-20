BEGIN;
-- Historical identifiers intentionally survive user/document/tenant deletion.
-- This is operator-only data: no audit MCP tool or caller-supplied event fields.
CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp TIMESTAMPTZ NOT NULL,
    request_id TEXT NOT NULL CHECK (request_id ~ '^[0-9a-f]{32}$'),
    user_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    tool TEXT NOT NULL CHECK (tool IN ('whoami','list_accessible_sources','get_document','search_knowledge','unknown')),
    action TEXT NOT NULL CHECK (action IN ('identity','read','unknown')),
    resource_id UUID,
    decision TEXT NOT NULL CHECK (decision IN ('ALLOW','DENY','ERROR')),
    reason TEXT NOT NULL CHECK (reason IN ('authorized','not_found_or_forbidden','unauthenticated','invalid_arguments','service_unavailable','unknown_tool')),
    query_hash TEXT CHECK (query_hash ~ '^[0-9a-f]{64}$'),
    duration_ms BIGINT NOT NULL CHECK (duration_ms >= 0)
);
CREATE INDEX audit_log_tenant_time ON audit_log(tenant_id,timestamp,id);
CREATE INDEX audit_log_request ON audit_log(request_id);
COMMIT;
