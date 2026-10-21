BEGIN;
-- Distinct document IDs a successful resource call returned to the caller.
-- NULL means not applicable (whoami, DENY, ERROR); an empty array means nothing was disclosed.
ALTER TABLE audit_log ADD COLUMN disclosed_document_ids UUID[];
ALTER TABLE audit_log ADD CONSTRAINT audit_log_disclosures_check CHECK (
    disclosed_document_ids IS NULL OR (
        decision = 'ALLOW'
        AND tool IN ('list_accessible_sources','get_document','search_knowledge')
        AND cardinality(disclosed_document_ids) <= 100
        AND array_position(disclosed_document_ids, NULL) IS NULL
    )
);
-- Answers "which callers received document X?" with = ANY / @> queries.
CREATE INDEX audit_log_disclosures ON audit_log USING GIN (disclosed_document_ids);
COMMIT;
