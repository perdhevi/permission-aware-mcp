BEGIN;
-- Composite keys make cross-tenant membership and grants impossible.
ALTER TABLE users ADD CONSTRAINT users_tenant_id_id_key UNIQUE (tenant_id, id);
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name),
    UNIQUE (tenant_id, id)
);
CREATE TABLE user_roles (
    tenant_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role_id UUID NOT NULL,
    PRIMARY KEY (user_id, role_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id) REFERENCES roles(tenant_id, id) ON DELETE CASCADE
);
CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    title TEXT NOT NULL,
    source TEXT NOT NULL,
    content TEXT NOT NULL,
    classification TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, source),
    UNIQUE (tenant_id, id)
);
CREATE TABLE document_acl (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    document_id UUID NOT NULL,
    user_id UUID,
    role_id UUID,
    permission TEXT NOT NULL CHECK (permission = 'read'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (num_nonnulls(user_id, role_id) = 1),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, role_id) REFERENCES roles(tenant_id, id) ON DELETE CASCADE,
    UNIQUE (document_id, user_id),
    UNIQUE (document_id, role_id)
);
CREATE INDEX documents_tenant_order ON documents(tenant_id, title, id);
CREATE INDEX user_roles_role ON user_roles(tenant_id, role_id, user_id);
INSERT INTO roles(tenant_id, name)
SELECT t.id, r.name FROM tenants t CROSS JOIN
    (VALUES ('employee'), ('engineering'), ('hr'), ('executive')) r(name) WHERE t.slug='acme';
INSERT INTO user_roles(tenant_id, user_id, role_id)
SELECT u.tenant_id, u.id, r.id FROM users u JOIN roles r ON r.tenant_id=u.tenant_id
WHERE u.issuer='http://dev.local' AND
    ((u.subject='alice' AND r.name='engineering') OR
     (u.subject='bob' AND r.name='hr') OR
     (u.subject='carol' AND r.name='executive') OR
     (u.subject='dave' AND r.name='employee'));
INSERT INTO documents(tenant_id, title, source, content, classification)
SELECT t.id, d.title, d.source, d.content, d.classification FROM tenants t CROSS JOIN (VALUES
    ('Company Handbook', 'company-handbook', 'Welcome to Acme. Treat colleagues respectfully and protect company information.', 'internal'),
    ('Engineering Roadmap', 'engineering-roadmap', 'Engineering plans: improve deployment automation, service reliability, and developer tooling.', 'confidential'),
    ('Salary Guidelines', 'salary-guidelines', 'HR reviews compensation bands annually. Individual compensation records are confidential.', 'confidential'),
    ('Board Strategy', 'board-strategy', 'The board reviews long-term investment priorities and strategic partnerships.', 'restricted')
) d(title, source, content, classification) WHERE t.slug='acme';
INSERT INTO document_acl(tenant_id, document_id, role_id, permission)
SELECT d.tenant_id, d.id, r.id, 'read' FROM documents d JOIN roles r ON r.tenant_id=d.tenant_id
JOIN tenants t ON t.id=d.tenant_id
WHERE t.slug='acme' AND
    (d.source='company-handbook' OR r.name='executive' OR
     (d.source='engineering-roadmap' AND r.name='engineering') OR
     (d.source='salary-guidelines' AND r.name='hr'));
COMMIT;
