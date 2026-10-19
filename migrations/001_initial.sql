BEGIN;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    issuer TEXT NOT NULL,
    subject TEXT NOT NULL,
    display_name TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);
INSERT INTO tenants (slug, name) VALUES ('acme', 'Acme');
INSERT INTO users (tenant_id, issuer, subject, display_name)
SELECT t.id, 'http://dev.local', s.subject, initcap(s.subject)
FROM tenants t CROSS JOIN (VALUES ('alice'), ('bob'), ('carol'), ('dave')) AS s(subject)
WHERE t.slug = 'acme';
COMMIT;
