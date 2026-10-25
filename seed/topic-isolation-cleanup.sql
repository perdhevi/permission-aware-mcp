-- Removes the optional Project Falcon fixture. Grants and chunks cascade.
-- Audit rows keep their historical disclosed document IDs.
BEGIN;
DELETE FROM documents d USING tenants t
WHERE d.tenant_id=t.id AND t.slug='acme' AND d.source IN
    ('falcon-overview', 'falcon-technical-design', 'falcon-staffing-plan', 'falcon-budget-approval');
COMMIT;
