-- Optional local Keycloak fixture. Preserve the original dev identities and ACLs.
BEGIN;
INSERT INTO users(tenant_id, issuer, subject, display_name)
SELECT t.id, 'http://127.0.0.1:8081/realms/permission-mcp', s.subject, s.name
FROM tenants t CROSS JOIN (VALUES
 ('10000000-0000-0000-0000-000000000001', 'Alice'),
 ('10000000-0000-0000-0000-000000000002', 'Bob'),
 ('10000000-0000-0000-0000-000000000003', 'Carol'),
 ('10000000-0000-0000-0000-000000000004', 'Dave')
) s(subject, name) WHERE t.slug='acme'
ON CONFLICT (issuer, subject) DO NOTHING;
INSERT INTO user_roles(tenant_id, user_id, role_id)
SELECT u.tenant_id, u.id, r.id FROM users u JOIN roles r ON r.tenant_id=u.tenant_id
WHERE u.issuer='http://127.0.0.1:8081/realms/permission-mcp' AND
 ((u.subject='10000000-0000-0000-0000-000000000001' AND r.name='engineering') OR
  (u.subject='10000000-0000-0000-0000-000000000002' AND r.name='hr') OR
  (u.subject='10000000-0000-0000-0000-000000000003' AND r.name='executive') OR
  (u.subject='10000000-0000-0000-0000-000000000004' AND r.name='employee'))
ON CONFLICT DO NOTHING;
COMMIT;
