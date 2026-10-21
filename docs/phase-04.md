# Phase 04 — Audit and security demo

## Scope and outcome

Persist every authenticated MCP tool decision, including denied direct reads and
identity-spoofing schema rejections. Demonstrate that a host following instructions
in authorized engineering text still cannot read a confidential document. Extend
the existing identity, tenant, RBAC, ACL and permission-filtered retrieval tests
without changing the original four-document matrix or adding end-user writes.

## Changes by file/component

| Files | Changes |
| --- | --- |
| `migrations/005_audit_disclosures.sql` (revision 2026-10-21) | Nullable `disclosed_document_ids UUID[]`, ALLOW/resource-tool-only and ≤100-entry constraint, GIN index. |
| `migrations/004_audit.sql` | Transactional audit table, bounded categorical fields, UUID identifiers, hash constraints, tenant/time and request indexes. |
| `internal/audit/service.go` | Event/store contract and request-local decision classification from typed internal errors. |
| `internal/repository/postgres/audit.go` | Parameterized persistent insert; absent resource/hash values become SQL NULL. |
| `internal/mcpserver/audit.go` | Receiving middleware outside SDK schema validation; trusted identity/request correlation, query hashing, centralized operational logs, bounded detached audit write, results withheld on failure. |
| `internal/mcpserver/server.go`, `cmd/server/main.go` | Require an audit-store dependency and wire PostgreSQL into the real server. |
| `internal/mcpserver/tools/{documents,search,whoami}.go` | Preserve internal error categories before sanitization; replace previous individual tool logs with middleware records. |
| `internal/mcpserver/audit_test.go` | Both protocol revisions, decision categories, content omission, storage outage, cancellation, and combined real PostgreSQL/MCP injection/tenant/spoofing test. |
| `internal/mcpserver/{documents,server}_test.go` | Supply isolated memory audit stores to existing HTTP tests. |
| `seed/prompt-injection.sql` | Optional append-once hostile engineering text referencing the actual confidential UUID, preserving source IDs and grants. |
| `seed/prompt-injection-demo.ps1` | Reproducible simulated-host follow-up, denial, spoofing rejection and identity check using Alice's development token. |
| README, seed notes, architecture, security, demo, Phase 02/03 guides | Upgrade instructions, audit boundary, demonstrator and revalidation record. |

## Design decisions

The SDK receiving middleware surrounds tool schema validation as well as handler
execution. Auditing only inside typed handlers would miss rejected identity/role/
tenant spoofing. All four tools are covered; unknown tool names become `unknown`
rather than arbitrary user-controlled log strings. MCP non-tool methods, HTTP
authentication failures and malformed messages rejected before tool dispatch are
outside the persistent tool audit boundary. HTTP operational logging still applies.

Events store timestamp, server-generated request ID, database caller and tenant
UUIDs, fixed tool/action, requested valid document UUID when applicable, decision,
fixed reason, SHA-256 of the exact search string, and execution duration in integer
milliseconds. Neither caller identity nor request correlation comes from arguments
or metadata. No token, raw query, document content/title, arguments, embeddings,
raw error or ACL is persisted. SHA-256 is unsalted correlation, not secrecy against
guessing common queries; audit records require trusted operator access.

ALLOW means the tool execution succeeded, including empty lists/searches. DENY
covers rejected inputs, unknown tools, missing/forbidden documents and missing
identity. ERROR covers internal storage/embedding/cancellation failures. Typed
handler errors are classified before sanitization; schema rejection falls back to
DENY `invalid_arguments`. Missing and forbidden documents use the same internal
reason `not_found_or_forbidden`; no existence probe is added. Successful search
does not imply permission to read any other document.

Each record is inserted synchronously before returning the result. An independent
two-second context retains request context values but ignores read cancellation.
A failed or missing store returns only `resource service unavailable`, discarding
content and structured output, and logs a sanitized storage-failure message.
The three-second resource timeout and two-second audit timeout are separate.
Duration measures execution before audit insertion, not network delivery or total
HTTP latency. An ALLOW event records the read decision, not delivery confirmation.

The read and audit insert are separate operations; no distributed/atomic delivery
guarantee exists. A process crash can leave a gap, and an unavailable database
cannot record its own write failure. Historical IDs deliberately have no foreign
keys, retaining audit history after source, user or tenant deletion. The application
uses the development DB owner, so this is not a tamper-resistant audit system.

The demo is opt-in and appends text to the existing engineering document instead
of introducing a fifth seed document. ACLs and existing content are preserved.
The malicious instruction names a confidential UUID as attacker-supplied data;
that identifier's presence in an allowed document does not grant access. The
PowerShell script and test simulate the unsafe host action deterministically.
No LLM, external API or model-specific injection success claim is involved.

## Setup prerequisites

Go 1.27.1+, PowerShell 7, Docker Compose v2 with a running Linux engine, local
PostgreSQL, and migrations 001–003. Existing development signing keys can be reused.
Run from the repository root. Go does not load `.env` automatically. Tests require
localhost socket access; initial dependency/image downloads need network access.
Keep development database credentials and private signing keys operator-only.

## Ordered validation commands and expected results

### 1. Start and inspect the database

```powershell
go version
docker compose config --quiet
docker compose up -d --wait
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT to_regclass('public.chunks'),to_regclass('public.audit_log');"
```

Expect a healthy database. A fresh volume applies 001–004 in order. Existing
volumes never rerun migrations. If chunks exists and audit_log is null, apply 004
once; apply earlier missing migrations first following [seed notes](../seed/README.md):

```powershell
Get-Content -Raw migrations/004_audit.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
if ($LASTEXITCODE -ne 0) { throw 'Migration 004 failed; inspect before retrying' }
```

Expect COMMIT. Do not reapply an existing migration or delete the data volume.

### 2. Verify all phases with SQL integration enabled

```powershell
gofmt -l cmd internal
go vet ./...
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 -v ./...
git diff --check
```

Expect clean formatting/vet/diff and passing tests. Confirm execution of
`TestAuditedInjectionIntegration`, `TestDocumentsIntegration`,
`TestSearchIntegration`, and `TestUsersIntegration`. Without TEST_DATABASE_URL,
SQL suites skip; that does not validate persistence, ACLs or pgvector. Tests use
isolated fixtures and clean up their own audit rows. Existing search tests re-ingest
the seeded acme documents. Do not use a production database.

### 3. Run the optional host demo and inspect audit storage

Follow the exact fixture, ingestion, server, script and audit SELECT commands in
[demo](demo.md#audited-prompt-injection-demo-phase-4). Expect an Alice search ALLOW,
an injected direct read DENY with `resource not found`, spoofing DENY, and a final
whoami ALLOW with Alice unchanged. Audit inspection should show matching caller,
tenant and unique generated request IDs, requested target UUID, fixed reasons and
a 64-character query hash. The four-source permission matrix remains unchanged.

## Automated test coverage

- `TestAuditHTTPBoundary`: both MCP revisions; all four tools; exact hashes;
  ALLOW/DENY/ERROR; malformed/absent resources; sanitized backend failures;
  schema spoofing; invalid service inputs; embedding failures; unknown tool-name
  normalization; one event per call; no secret/body/query leakage; audit outage
  withholds previously authorized content and structured output.
- `TestAuditSurvivesCancellation`: cancellation still permits a bounded ERROR
  record through the detached write context.
- `TestAuditedInjectionIntegration`: real database users, documents, ACLs,
  ingestion, pgvector, HTTP MCP dispatch and PostgreSQL audit insertion together.
  Authorized hostile text is retrieved; a simulated host follows its target UUID;
  same-tenant confidential and foreign-tenant reads are denied; spoofing is rejected;
  subsequent authorized access still works. Both revisions produce the expected
  14 total events, 8 ALLOW, 4 generic document denials, 2 spoofing denials, 2 query
  hashes and 14 distinct request IDs. Disclosure columns show exactly the allowed
  document for each list/search/read ALLOW, NULL on the other 8 rows, and never the
  confidential or foreign document. The schema rejects disclosures on a DENY row.
  Fixtures and their events are removed.
- `TestAuditHTTPBoundary` also requires disclosures on every ALLOW resource call
  and nil disclosures for whoami, DENY and ERROR. `TestDisclosed` covers distinct
  ordering, empty-versus-not-applicable and copy semantics.
- Existing Phase 1–3 authentication, identity switching, tenant constraints,
  explicit grants, revocations, stale content, ingestion rollback, top-1 ranking
  and Unicode suites continue to run.

## Validation record — 2026-10-20

Started the existing Compose database and confirmed Phase 3 chunks existed with
no audit table. The existing uncached suite passed with PostgreSQL integration
enabled, revalidating Phases 2 and 3. Migration 004 then committed on that existing
database. The expanded verbose uncached suite passed, including real persistent
audit/injection integration and both protocol versions. The first sandboxed test
attempt failed because localhost sockets were blocked; the authorized rerun passed.

Also created temporary database `phase4_validation_20261020`, applied 001–004 in
order, and ran the optional fixture twice: UPDATE 1 followed by UPDATE 0. The
final verbose uncached full suite passed against this fresh database after the
mandatory-store wiring and centralized logging changes. `go vet ./...`, formatting,
`go build -o .local/phase04-server.exe ./cmd/server`, and `git diff --check` passed
(Git emits only its configured LF-to-CRLF conversion notices).

Ran ingestion and the built server on port 18084 with the fresh validation DB,
reused existing signing keys, and executed `seed/prompt-injection-demo.ps1` in
PowerShell 7.6.5. It passed using real minted development JWTs. Operator SQL
confirmed five persisted Alice events: two whoami ALLOW, one search ALLOW, one
generic document DENY and one spoofing DENY. Four documents and nine grants
remained; no orphan test events remained. The temporary server was stopped and
database removed. The development database remains running with migration 004;
the optional hostile fixture was applied only to the temporary validation database.
Ignored smoke executable/log files remain under `.local/`.

The commands above are reproducible instructions. Only explicitly recorded actions
were executed; no external LLM demonstration, load test or production deployment
was performed.

## Revision 2026-10-21 — disclosed documents

### Scope and changes

Audit rows previously recorded the requested UUID of a direct read but not what a
successful list or search returned, so "which callers received document X?" was
unanswerable. Migration 005 adds `disclosed_document_ids`. `list_accessible_sources`,
`search_knowledge` and `get_document` call `audit.RecordDisclosed` with the distinct
document UUIDs of their result, in result order; the middleware stores them only
when the decision is ALLOW. NULL means not applicable (whoami, DENY, ERROR); an
empty array means an authorized call returned nothing. Files: `internal/audit/service.go`,
`internal/mcpserver/audit.go`, `internal/mcpserver/tools/{documents,search}.go`,
`internal/repository/postgres/audit.go`, tests and the documents listed above.

### Design decisions

Document UUIDs, not chunk UUIDs, are stored: chunk IDs change on every re-ingest,
while document IDs are stable and match the ACL unit. Handlers report IDs through
the same request-local state as error categories, rather than the middleware
parsing result JSON. The column is document-level metadata only: no titles, content
or ACL details. It makes audit rows more sensitive (who saw what), consistent with
existing operator-only access. A failed audit write still withholds results.
Example operator query:

```sql
SELECT a.timestamp, u.subject, a.tool FROM audit_log a JOIN users u ON u.id = a.user_id
WHERE '<document-uuid>'::uuid = ANY(a.disclosed_document_ids) ORDER BY a.timestamp DESC;
```

### Upgrade and validation commands

Apply migration 005 once to existing databases following
[seed notes](../seed/README.md#upgrade-an-existing-phase-4-database); fresh volumes
apply it automatically. Then rerun section 2 above with `TEST_DATABASE_URL` set
and expect `TestDisclosed`, `TestAuditHTTPBoundary` and `TestAuditedInjectionIntegration`
to pass.

### Validation record — 2026-10-21

Created temporary database `review_validation_20261021b`, applied 001–004, inserted
one pre-existing audit row, then applied 005: COMMIT, the legacy row kept NULL
disclosures. Applied the OIDC user fixture and ran `go test -count=1 -v ./...` with
`TEST_DATABASE_URL` and `TEST_OIDC_ISSUER` against running Keycloak 26.8.1. All
packages passed, including the extended `TestAuditedInjectionIntegration`,
`TestAuditHTTPBoundary`, `TestDisclosed`, `TestKeycloakIntegration` and SQL suites,
except `TestLoginPKCECallback`, which could not bind 127.0.0.1:8765 because an
unrelated local container published that port (environmental; not affected by
this change). `gofmt -l cmd internal` and `go vet ./...` were clean. The temporary
database was dropped. The development database was **not** migrated; apply 005
there before running the server. No manual server/demo run was performed for
this revision.

## Troubleshooting

- `resource service unavailable` for every otherwise valid tool: inspect server
  logs for `audit storage unavailable`, verify migrations 004 and 005 and INSERT privileges,
  pool availability and database connectivity. No content is returned without a
  successful audit write. Readiness checks connectivity, not schema or audit writes.
- Missing audit for 401 or malformed JSON: these fail before authenticated tool
  dispatch and intentionally have no persistent tool event.
- No hostile search hit: apply the optional fixture to the correct acme database,
  re-ingest, and ensure Engineering Roadmap is readable by Alice. Content hash
  checks intentionally suppress stale chunks immediately after a content edit.
- Duplicate fixture run reports UPDATE 0: expected after the marker was appended.
  A missing acme/source pair also updates zero rows; inspect before assuming success.
- Skipped SQL tests: set TEST_DATABASE_URL and rerun with localhost access.
- Docker access/pipe errors: start Docker Desktop and ensure the Linux engine is
  reachable. Restricted runs may require approval for Docker and localhost sockets.

## Limitations and deferred work

Phase 5 compatibility update: `seed/prompt-injection-demo.ps1` now accepts
`-Token $token` for Alice's Keycloak access token, recognizes the fixture's stable
subject, and checks the final subject against the initial authenticated subject.
Without a token it preserves dev minting; OIDC mode requires an explicit token.
The changed script was syntax-checked during Phase 5; that new branch was not
executed as a full PowerShell/Keycloak demo. The earlier Phase 4 validation record
above remains historical. See [Phase 05](phase-05.md) for actual OIDC test results.

Development authentication, a small exact-search corpus, and trusted DB operators
remain the scope. Disclosures are document-level: they do not record which chunks
or how many characters were returned. No production OAuth, administrative audit MCP tool, audit export,
retention/deletion policy, immutable storage, separate audit credentials, retries,
crash recovery queue, authenticated HTTP-failure audit, LLM generation or live-model
injection evaluation. Rate limiting and audit-volume controls remain future work.
The optional content fixture has no automatic backup/undo; save customized content
before applying it, restore that copy when finished, and re-ingest.
