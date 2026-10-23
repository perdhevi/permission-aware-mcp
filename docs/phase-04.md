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
| `seed/audit-disclosure-demo.ps1` (revision 2026-10-21) | End-to-end replication: four seeded callers, every tool, row-by-row audit comparison, operator query and DENY constraint check. |
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

### Test and replication steps

Run from the repository root in PowerShell 7 (Windows PowerShell 5.1 also works)
with Docker running. Use a local
development database only; the replication script adds 32 audit rows.

**1. Unit tests (no database).**

```powershell
gofmt -l cmd internal
go vet ./...
go test -count=1 -v -run 'TestDisclosed|TestAuditHTTPBoundary|TestAuditSurvivesCancellation' ./internal/audit ./internal/mcpserver
```

Expect no gofmt output and PASS for all three tests. `TestDisclosed` checks nil
(not applicable) versus empty (nothing returned), distinct IDs in result order,
and copy semantics. `TestAuditHTTPBoundary` checks both MCP revisions: every ALLOW
from list/search/read records exactly the returned document, while whoami, DENY
and ERROR record nil.

**2. Database and migration 005.** Start Compose and check the column:

```powershell
docker compose up -d --wait
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT count(*) FROM information_schema.columns WHERE table_name='audit_log' AND column_name='disclosed_document_ids';"
```

Expect `1` on a fresh volume. If `0`, apply 005 once following
[seed notes](../seed/README.md#upgrade-an-existing-phase-4-database) and expect
`COMMIT`; rerun the check. Never reapply it once the count is 1.

**3. Integration tests (real PostgreSQL).**

```powershell
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 -v -run 'TestAuditedInjectionIntegration|TestDocumentsIntegration|TestSearchIntegration' ./...
```

Expect PASS, not SKIP. `TestAuditedInjectionIntegration` asserts, for both
revisions, 2 list/2 search/2 read rows disclosing exactly the allowed document,
8 NULL rows, zero rows disclosing the confidential or foreign-tenant document,
and that the schema rejects an INSERT with disclosures on a DENY row. Then run
the whole suite (`go test -count=1 ./...`); see the `TestLoginPKCECallback`
note in Troubleshooting if port 8765 is taken.

**4. End-to-end replication against the seeded matrix.** In terminal 1:

```powershell
$env:AUTH_MODE = 'dev'
go run ./cmd/ingest --tenant acme
go run ./cmd/devtoken keygen   # only once; it refuses to overwrite existing keys
go run ./cmd/server
```

In terminal 2 (also `AUTH_MODE=dev` or unset), with no other MCP clients running:

```powershell
./seed/audit-disclosure-demo.ps1
```

The script mints development tokens for Carol, Alice, Bob and Dave. For each it
calls `whoami`, `list_accessible_sources`, `search_knowledge` (`salary`, limit 20),
`get_document` on all four seeded documents and one spoofed `get_document`. It
records the expected audit row per call, then compares them in order with the
`audit_log` rows written after the run started:

| Call | Decision | `disclosed_document_ids` |
| --- | --- | --- |
| `whoami` | ALLOW | NULL |
| `list_accessible_sources` | ALLOW | UUIDs of the caller's listed sources, in listing order |
| `search_knowledge` | ALLOW | Distinct UUIDs of the returned chunks' documents, in rank order |
| `get_document`, permitted | ALLOW | That single UUID |
| `get_document`, forbidden | DENY | NULL |
| `get_document` with `role`/`user_id` | DENY | NULL |

It also runs the operator query for Salary Guidelines and expects exactly `bob`
and `carol`, and checks that a disclosure on a DENY row is rejected in a
rolled-back transaction. Expect one line starting `PASS: 32 audit rows`. Any
mismatch throws with the expected and actual row. Use `-Endpoint` for another
server address. The script requires `AUTH_MODE=dev`; OIDC replication is
manual (use Alice's login token with the README requests and the SQL below).

**5. Manual inspection (optional).** As a trusted operator:

```powershell
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT a.timestamp,u.subject,a.tool,a.decision,a.reason,a.resource_id,a.disclosed_document_ids FROM audit_log a JOIN users u ON u.id=a.user_id ORDER BY a.timestamp DESC LIMIT 32;"
```

Rows written before migration 005 keep NULL disclosures.

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

### Validation record — 2026-10-23 (test and replication steps)

Added `seed/audit-disclosure-demo.ps1` and the steps above. Executed: the script
parses without errors in Windows PowerShell 5.1; `gofmt -l cmd internal` and
`go vet ./...` were clean; `go test -count=1 ./...` passed every package except
`cmd/login`, where `TestLoginPKCECallback` again could not listen on
127.0.0.1:8765 (environmental, as in the 2026-10-21 record). Docker was not
running at first, so the SQL suites skipped in that run. Steps 2–5 were then run
the same day, recorded below.

Step 2: started the existing Compose development database. Migration 004 was present
and 005 was missing (column count 0, `audit_log` empty). Applied 005 once: BEGIN,
2× ALTER TABLE, CREATE INDEX, COMMIT. Recheck returned 1. **The development
database now has migration 005**, which supersedes the 2026-10-21 note that it
had not been migrated.

Step 3: with `TEST_DATABASE_URL` set, `TestAuditedInjectionIntegration`,
`TestDocumentsIntegration` and `TestSearchIntegration` ran (not skipped) and
passed. The full `go test -count=1 ./...` passed every package except `cmd/login`.
`TestLoginPKCECallback` failed because an unrelated local `python` process was
listening on 127.0.0.1:8765. That process was left running.

Step 4: `go run ./cmd/ingest --tenant acme` ingested 4 documents (dev-hash-v1,
128 dimensions). Built and ran the server in `AUTH_MODE=dev` on 127.0.0.1:8080
with the existing signing keys. `/readyz` returned 200. The script was run with
Windows PowerShell 5.1 (`powershell -File`). The first two attempts found two
script bugs, both fixed in `seed/audit-disclosure-demo.ps1`:

- A one-row SQL result came back as a plain string, so `[0]` read the marker as `-`.
- In 5.1, the expected psql error on stderr became a terminating error under
  `ErrorActionPreference = 'Stop'`.

The server and audit storage were not at fault in either attempt: each attempt
wrote its 32 rows before the script failed. The third run printed
`PASS: 32 audit rows after 2026-10-23 09:10:40.31952+00 match the permission matrix;
Salary Guidelines reached only bob and carol; DENY disclosures are rejected.` No
row with the probe request ID `000…0` was committed.

Step 5: the operator query, joined to document titles, matched the expected table.
Carol listed and searched all four documents. Alice listed and searched Company
Handbook and Engineering Roadmap, Bob listed and searched Company Handbook and
Salary Guidelines, and Dave only Company Handbook. Every permitted read disclosed
exactly its document. All forbidden reads (`not_found_or_forbidden`), spoofed reads
(`invalid_arguments`) and whoami rows had NULL disclosures. The database held 96
audit rows (three runs × 32) with 96 distinct request IDs. The rows were kept, and
the server was stopped. The optional prompt-injection fixture was not applied.
Not exercised: OIDC mode, PowerShell 7 and the injection demo script.

## Troubleshooting

- `resource service unavailable` for every otherwise valid tool: inspect server
  logs for `audit storage unavailable`, verify migrations 004 and 005 and INSERT privileges,
  pool availability and database connectivity. No content is returned without a
  successful audit write. Readiness checks connectivity, not schema or audit writes.
- Missing audit for 401 or malformed JSON: these fail before authenticated tool
  dispatch and intentionally have no persistent tool event.
- `audit-disclosure-demo.ps1` reports an audit row count mismatch: another client
  (or a test run) wrote audit rows during the run. Stop other clients and rerun;
  each run compares only rows newer than the latest row at its start.
- The script reports `search failed; ingest acme first`: run
  `go run ./cmd/ingest --tenant acme`. If the injection fixture was applied, the
  extra Engineering Roadmap text does not change the expected disclosures.
- `TestLoginPKCECallback` fails with `cannot listen on 127.0.0.1:8765`: another
  process holds the CLI callback port (`Get-NetTCPConnection -LocalPort 8765`).
  It is unrelated to audit disclosures; free the port and rerun `./cmd/login`.
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
