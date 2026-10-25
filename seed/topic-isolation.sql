-- Optional operator-run demo. Adds one topic ("Project Falcon") with four documents:
-- an overview every role can read, and three confidential documents each granted to
-- exactly one named user (no role grant), so the same query returns different
-- documents per caller. Preserves existing documents, grants and IDs; safe to rerun.
-- Re-ingest acme afterward. Remove with seed/topic-isolation-cleanup.sql.
BEGIN;
INSERT INTO documents(tenant_id, title, source, content, classification)
SELECT t.id, d.title, d.source, d.content, d.classification FROM tenants t CROSS JOIN (VALUES
    ('Project Falcon Overview', 'falcon-overview', 'Project Falcon is the 2027 platform migration. Project Falcon moves customer workloads to the new region in three waves.', 'internal'),
    ('Project Falcon Technical Design', 'falcon-technical-design', 'Project Falcon technical design: the migration cuts over databases with logical replication, then switches traffic service by service.', 'confidential'),
    ('Project Falcon Staffing Plan', 'falcon-staffing-plan', 'Project Falcon staffing plan: the migration hires six contractors and moves two engineers from support for the three waves.', 'confidential'),
    ('Project Falcon Budget Approval', 'falcon-budget-approval', 'Project Falcon budget approval: the board approved 4.2 million for the migration, released wave by wave.', 'restricted')
) d(title, source, content, classification)
WHERE t.slug='acme'
ON CONFLICT (tenant_id, source) DO NOTHING;
INSERT INTO document_acl(tenant_id, document_id, role_id, permission)
SELECT d.tenant_id, d.id, r.id, 'read' FROM documents d JOIN roles r ON r.tenant_id=d.tenant_id
JOIN tenants t ON t.id=d.tenant_id
WHERE t.slug='acme' AND d.source='falcon-overview'
ON CONFLICT DO NOTHING;
-- User-level grants: each confidential document has exactly one reader.
INSERT INTO document_acl(tenant_id, document_id, user_id, permission)
SELECT d.tenant_id, d.id, u.id, 'read' FROM documents d
JOIN users u ON u.tenant_id=d.tenant_id AND u.issuer='http://dev.local'
JOIN tenants t ON t.id=d.tenant_id
WHERE t.slug='acme' AND
    ((d.source='falcon-technical-design' AND u.subject='alice') OR
     (d.source='falcon-staffing-plan' AND u.subject='bob') OR
     (d.source='falcon-budget-approval' AND u.subject='carol'))
ON CONFLICT DO NOTHING;
COMMIT;
