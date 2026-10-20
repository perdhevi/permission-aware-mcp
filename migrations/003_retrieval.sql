BEGIN;
CREATE TABLE chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    document_id UUID NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    content TEXT NOT NULL CHECK (char_length(content) BETWEEN 1 AND 1000),
    embedding vector(128) NOT NULL CHECK (vector_norm(embedding) > 0),
    embedding_model TEXT NOT NULL CHECK (embedding_model = 'dev-hash-v1'),
    embedding_dimensions INTEGER NOT NULL CHECK (embedding_dimensions = 128),
    document_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE,
    UNIQUE (document_id, ordinal)
);
CREATE INDEX chunks_tenant_document ON chunks(tenant_id, document_id);
COMMIT;
