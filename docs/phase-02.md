# Phase 02 — Roles and document authorization

## Scope and outcome

Build on [Phase 01](phase-01.md) with tenant-scoped database roles, role membership,
documents, explicit read ACLs, an authorization service, and the MCP tools
`list_accessible_sources` and `get_document`. `whoami` now includes database roles.
The caller still comes exclusively from a verified bearer token and local identity.

| Caller | Database role | Readable sources |
| --- | --- | --- |
| alice | engineering | Company Handbook, Engineering Roadmap |
| bob | hr | Company Handbook, Salary Guidelines |
| carol | executive | Board Strategy, Company Handbook, Engineering Roadmap, Salary Guidelines |
| dave | employee | Company Handbook |

## Changes by file/component

| Files | Change |
| --- | --- |
| `migrations/002_authorization.sql` | Transactional upgrade: roles, user_roles, documents, document_acl, tenant-aware composite foreign keys, indexes, four role assignments, four documents, nine grants. |
| `internal/domain/user.go`, `internal/domain/document.go` | Database roles and typed source/document results. Source output contains only UUID and title; document output adds bounded content and truncation status. |
| `internal/repository/repository.go` | Define permission-constrained document repository methods and generic not-found sentinel. |
| `internal/repository/postgres/users.go` | Load sorted role names with the user in one statement. |
| `internal/repository/postgres/documents.go` | Share a tenant/active-user/ACL predicate between listing and direct reads. Filter before ordering/limit; bound returned content in PostgreSQL. |
| `internal/authn/identity.go` | Copy database roles into each fresh principal. |
| `internal/authz/service.go`, `doc.go` | Separate resource authorization orchestration from authentication and transport; validate principals, UUIDs and bounds, delegate atomic authorized reads. |
| `internal/mcpserver/tools/documents.go` | Strict input schemas, defaults, generic errors, three-second read timeouts, request/caller-correlated decision logs. |
| `internal/mcpserver/tools/whoami.go` | Add copied database roles to output. |
| `internal/mcpserver/server.go`, `cmd/server/main.go` | Inject the authorization service and register both tools. |
| `internal/authz/service_test.go` | Principal/input boundaries, trusted repository parameters, no role bypass or cached reads. |
| `internal/repository/postgres/documents_test.go` | Real seed matrix, user/role grants, revocation, active status, tenant isolation, foreign keys, Unicode limits. |
| `internal/mcpserver/documents_test.go`, `server_test.go` | Both protocol versions, caller isolation, input schemas, generic errors and the existing identity checks. |
| `internal/authn/identity_test.go` | Verify roles originate in the repository and are copied. |
| `README.md`, `seed/README.md`, `docs/architecture.md`, `security.md`, `demo.md`, phase guides | Update setup, explicit upgrade instructions, design, demonstration, and current behavior. Correct the historical Compose project-name note to match configuration. |

## Design decisions

Access requires all three conditions: document tenant equals authenticated tenant;
the local user is still active in that tenant; and an explicit read grant matches
the user or one of their current database roles. No ACL means denial. Roles have
no inheritance or privileged bypass: executive access is seeded with explicit grants.
Classification is descriptive metadata, never an implicit grant.

The service uses permission-constrained repository reads instead of a separate
`CanRead` check followed by an unrestricted fetch. Each SQL statement checks the
current memberships and ACLs while selecting data. This avoids a permission-check/
fetch race and applies revocations on the next query. Principal role names are
informational for `whoami`; SQL does not trust a role list passed by a caller.
There is no cache. PostgreSQL statement snapshots define concurrent-change visibility;
a revocation committed after a read begins cannot retract data already returned.

ACLs use exactly one of `user_id` and `role_id`, with a database check constraint.
This intentionally refines the roadmap's polymorphic principal ID into real foreign
keys. Composite `(tenant_id, id)` foreign keys prohibit cross-tenant memberships
and grants. Only `read` permission is supported. Grants are additive; explicit deny
entries and role inheritance are unsupported. `EXISTS` prevents duplicate rows.

Lists return `sources: []` when empty, sorted by title then UUID. `limit` defaults
to 20 and accepts 1–100. There is no count, offset or continuation token. Direct
reads require a document UUID; `max_chars` defaults to 12000 and accepts 1–50000.
PostgreSQL `left`/`char_length` bound Unicode characters rather than bytes; combining
sequences can still be split. The output is `{document_id,title,content,truncated}`.

Missing and forbidden documents, including syntactically invalid UUIDs that reach
the service, return `resource not found`. Schema-invalid arguments produce normal
MCP validation errors. Database failures become `resource service unavailable`;
no raw errors or partial results reach clients. A tool error is an MCP result
with `isError: true`, not necessarily an HTTP error status. Both tools reject extra
identity, tenant and role fields. No content, token, SQL diagnostic or ACL is logged.
Basic decision logs are operational logs; persistent audit storage remains Phase 4.

## Setup prerequisites

Use Go 1.27.1+, Docker Compose v2, a running Linux Docker engine, and PowerShell 7
for the examples. Run from the repository root. Initial image/dependency downloads
need network access. Tests need localhost socket access; database tests also need
PostgreSQL at localhost:5432. Reuse Phase 1's signing keys. Defaults are development
only, and Go does not load `.env` automatically.

## Ordered validation commands and expected results

### 1. Start PostgreSQL and inspect migration state

```powershell
go version
docker compose config --quiet
docker compose up -d --wait
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT to_regclass('public.users') AS users, to_regclass('public.roles') AS roles;"
```

Expect a healthy container. A fresh volume applies 001 and 002 in filename order;
both relations exist. If an existing phase 1 volume has users but no roles, upgrade:

```powershell
Get-Content -Raw migrations/002_authorization.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
if ($LASTEXITCODE -ne 0) { throw 'Migration 002 failed' }
```

Expect `COMMIT`. Do not run this again when roles already exists. Migrations are
transactional, non-repeatable SQL files. There is no versioned upgrade runner or
automatic application at server startup. See [seed notes](../seed/README.md) for
an existing empty schema. Preserve existing volumes; restarting does not migrate them.

### 2. Confirm seed state

```powershell
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT u.subject,r.name FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id JOIN tenants t ON t.id=u.tenant_id WHERE t.slug='acme' ORDER BY u.subject;"
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT d.title,count(a.id) AS grants FROM documents d JOIN tenants t ON t.id=d.tenant_id LEFT JOIN document_acl a ON a.document_id=d.id WHERE t.slug='acme' GROUP BY d.id ORDER BY d.title;"
```

Expect the four assignments from the matrix. Grant counts in title order are:
Board Strategy 1, Company Handbook 4, Engineering Roadmap 2, Salary Guidelines 2.
No role inheritance is needed. Seed UUIDs are database-generated.

### 3. Run automated checks with database integration enabled

```powershell
gofmt -l cmd internal
go vet ./...
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 -v ./...
```

Expect clean formatting and vet, and every test to pass. Confirm
`TestDocumentsIntegration` and `TestUsersIntegration` actually run rather than skip.
Integration tests use dedicated fixtures and clean up without changing seed users.
If the development seed is intentionally customized, the exact seed-matrix tests
require a separate database initialized with the original two migrations.

### 4. Exercise the CLI, running server and tools

```powershell
# Only if neither key exists:
go run ./cmd/devtoken keygen
# Start and leave running; reuse keys if already present:
go run ./cmd/server
```

In a second terminal, run the exact [README](../README.md) `whoami`, list and direct
read examples. Expect Alice's database role `engineering`, two accessible sources,
and Company Handbook content. Use `max_chars = 5` to see five characters and
`truncated: true`. Omit it to use the default.

Repeat listing with only the bearer token changed to each of Bob, Carol and Dave;
expect the matrix above. As Carol, save Salary Guidelines' UUID. Switch back to
Alice's token and request that UUID: expect `resource not found`, with no title or
content. Bob similarly cannot fetch Engineering Roadmap. A random well-formed UUID
produces the same not-found message. Extra `role`, `user_id` or `tenant_id` inputs,
limit 0/101, and max_chars 0/50001 must fail.

## Automated test coverage

- `TestServiceBoundary`: invalid/missing principals and bad bounds never reach
  storage; malformed UUIDs are not found; repository errors propagate internally;
  principal role claims cannot bypass storage and reads are not cached.
- `TestDocumentsIntegration`: full four-caller matrix for lists and direct reads;
  no-ACL default denial; direct user grants; roles require actual membership;
  user/role ACL and membership revocation; inactive users; nonexistent documents;
  foreign-tenant documents/principals; cross-tenant membership/user/role ACL foreign
  keys; Unicode truncation and exact-bound behavior.
- `TestHTTPDocumentBoundary`: official SDK clients using `2026-07-28` and
  `2025-11-25`; Alice/Bob/Alice caller switching; empty results; truncated direct
  reads; required/unknown/type/range argument validation; identity/tenant spoofing;
  malformed/missing resources; sanitized repository errors. Repository is a fake
  in this HTTP test; real SQL policy is covered by the integration test.
- Existing JWT, identity, health, CLI key and configuration suites still run.
  `TestResolve` additionally verifies copied database roles.

## Validation record — 2026-10-20

Executed: formatting, `go vet ./...`, and the uncached full suite with
`TEST_DATABASE_URL` enabled. All tests passed, including both PostgreSQL integration
tests and both SDK protocol versions. Docker Desktop initially was stopped;
starting it outside the sandbox succeeded. The default Compose database initialized
both migrations on a fresh volume and became healthy.

Also executed 001 followed by 002 explicitly in an isolated
`phase2_upgrade_validation` database: both committed, with 4 users, 4 roles,
4 documents and 9 read grants. That temporary database was removed afterward.
This validates upgrading without deleting an existing schema. Seed role assignments
and per-document grant counts were also inspected through psql.

A live smoke test built and started the server on port 18082, reused local keys,
minted tokens through `cmd/devtoken`, verified each user's identity/database role,
confirmed source counts 4/2/2/1 for Carol/Alice/Bob/Dave, read bounded content, and
confirmed Alice's Salary Guidelines request returned only `resource not found`.
The temporary server was stopped afterward; the Compose database remains running.
The smoke helper and executable are ignored under `.local/`.

The manual validation commands above are reproducible instructions;
this record does not imply every manual variant was individually executed.

## Phase 4 revalidation — 2026-10-20

Reviewed the shared SQL policy and service boundary, then reran the uncached full
suite with TEST_DATABASE_URL against the existing development database and a
fresh database initialized with 001–004. `TestDocumentsIntegration`, identity and
HTTP caller-switching tests passed, including explicit user/role grants, revocation,
active status, tenant constraints, generic direct-read denial and Unicode limits.
No authorization-policy defect was found in this review; this is evidence from
the reviewed paths and test cases, not a proof for every possible deployment.

Phase 4 replaces the original handler-local operational logs with required
persistent audit middleware. Schema rejections now receive DENY records too;
typed service errors retain internal categories before client sanitization.
Resource SQL, ACL semantics, tool arguments and successful outputs are unchanged.
The current implementation also includes Phase 3 ingestion/search; references
above to later phases describe the original Phase 2 delivery scope. See
[Phase 04](phase-04.md) for audit behavior, prerequisites and validation.

## Troubleshooting

- Docker pipe/socket errors: start Docker Desktop and ensure the Linux engine is
  available. Restricted environments need approval for Docker and localhost tests.
- `relation roles does not exist` or a generic identity lookup failure after updating:
  apply 002 to the correct database. `/readyz` checks connectivity, not schema.
- An existing-table/constraint migration error: stop retrying and inspect the
  schema. Reapplying 002 is intentionally rejected and its transaction rolls back.
- Empty lists: check active user, tenant, current user_roles and document_acl.
  Role names in JWTs or MCP arguments never grant access. Changing the development
  issuer requires matching local identities and explicit role assignments.
- `resource not found`: intentionally does not distinguish forbidden from missing.
  Use authorized database inspection to diagnose; never add an existence probe
  that exposes confidential titles to the caller.
- Database tests skip without `TEST_DATABASE_URL`; a green skipped suite does not
  validate SQL policy. A stale/customized seed can fail the exact matrix assertions.
- Compose currently uses `permission-aware-mcp_postgres_data`; historical volumes
  under a different project name remain separate. Inspect before changing volume
  configuration or assuming previous data was removed.

## Limitations and deferred work

Development authentication only. No external OAuth, role administration tools,
document editing, automatic upgrade runner, RLS, pagination, or policy cache.
Document data is small SQL seed content, with no ingestion pipeline, chunks,
embeddings or search until Phase 3. Persistent audit records and the complete
host-driven prompt-injection demonstration remain Phase 4. The database application
account can administer policy; database administrators remain trusted.
