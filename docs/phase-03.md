# Phase 03 — Ingestion and permission-filtered retrieval

## Scope and outcome

Add offline document chunking, a working embedding provider, trusted ingestion CLI,
pgvector storage, and the MCP tool `search_knowledge`. Retrieval uses the same
tenant/active-user/current ACL policy as direct reads, before ranking and LIMIT.
The bearer token remains the only source of caller identity. No LLM generation,
external embedding API, or new end-user write capability is introduced.

## Changes by file/component

| Files | Changes |
| --- | --- |
| `migrations/003_retrieval.sql` | Transactional chunks schema, tenant/document foreign key, ordinal uniqueness, 1000-character bound, 128-dimensional vector and fixed model constraints, source hash and lookup index. |
| `internal/rag/embedder.go`, `hash.go` | Provider contract and deterministic offline `dev-hash-v1`; normalized word hashing; finite, nonzero vector validation and parameterized vector text encoding. |
| `internal/rag/chunker.go` | Unicode windows of 1000 characters with 100-character overlap, skipping blank windows. |
| `internal/rag/retriever.go` | Validate authenticated principal and input, embed query, validate provider vector, delegate permission-constrained retrieval. |
| `internal/domain/search.go` | Chunk/document UUID, title, content and cosine distance output. |
| `internal/repository/postgres/search.go` | Materialized authorized candidate set, shared read predicate, compatible-model/content-hash filtering, exact cosine ranking and deterministic UUID tie breaks. |
| `internal/repository/postgres/ingest.go`, `cmd/ingest/main.go` | Read existing tenant documents, lock each row and atomically replace chunks; bounded execution and sanitized CLI errors. |
| `internal/mcpserver/tools/search.go`, `server.go`, `cmd/server/main.go` | Register strict read-only search tool, 3-second timeout, caller/request-correlated logs without raw queries or content, server wiring of local embedder. |
| `internal/rag/rag_test.go` | Hash consistency, Unicode chunking/overlap, invalid vectors, cancellation, principal/input boundary, trusted repository parameters and uncached reads. |
| `internal/repository/postgres/search_test.go` | Real ranking/policy boundary, ingestion repeatability and rollback, revocation, inactive/foreign principals, stale chunks, model compatibility, seeded caller matrix. |
| `internal/mcpserver/documents_test.go`, `server_test.go` | Search via both SDK protocols, caller switching, empty results, strict arguments; update constructor call sites. |
| `README.md`, `seed/README.md`, `docs/architecture.md`, `security.md`, `demo.md` | Setup/upgrade/ingestion/search examples, current behavior and limitations. |

## Design decisions

The local embedder lowercases Unicode letters/numbers, hashes each word using
FNV-1a into 128 count buckets, and normalizes the vector. Query and document
embedding use identical logic. This implements the roadmap's fake-provider demo
without network access, API keys or new dependencies. It demonstrates authorization
and vector plumbing; it has no synonym understanding and can produce hash collisions.
The model and dimension are intentionally fixed in code and schema. There are no
embedding environment settings to misconfigure; changing providers requires a
schema migration and re-ingestion as well as server wiring changes.

`search_knowledge` requires `query` (1–4000 Unicode characters, nonblank), and
optional `limit` (default 5, range 1–20). Extra fields are rejected. Queries with
no letters or numbers cannot produce a vector and return a generic service error.
Output is `{results:[{chunk_id,document_id,title,content,distance}]}`. Empty output
uses `[]`, not null. Lower cosine distance ranks first. There is no relevance
threshold, so unrelated authorized chunks can appear. There is no total count,
pagination, ACL metadata, or content from unauthorized candidates.

The repository shares Phase 2's `readable` predicate, including current database
role membership and active status. A `MATERIALIZED` CTE selects authorized chunks
before the outer distance calculation, sorting and LIMIT. There is no ANN index
and no top-K post-filter. Principal role names are informational and grant no access.
All SQL values, including vector literals, are parameters. Subsequent direct reads
still independently authorize. PostgreSQL statement snapshots determine visibility
of concurrent permission changes; already returned content cannot be retracted.

Ingestion is a trusted operator command using database credentials. `--tenant`
selects a tenant slug, not an MCP caller identity. The CLI ingests existing
`documents.content`; SQL migrations remain the demo source of documents and ACLs.
This avoids duplicating seed ownership in a manifest and preserves document IDs,
titles and permissions. It does not import Markdown files or create grants.

Each document is locked with `FOR UPDATE`, chunked, embedded, and replaced in one
transaction. Repeating ingestion replaces rather than appends, with new chunk UUIDs.
One failed document rolls back its chunks; earlier document commits remain. The
whole corpus is not atomic. Empty/blank documents produce zero chunks; a nonblank
window with no searchable terms fails that document. The CLI exits nonzero for an
unknown/empty tenant or an embedding/database failure. It has a two-minute overall
timeout and interrupt cancellation. Search has a three-second timeout.

Each chunk stores `md5(documents.content)` and search checks that the source still
matches. Content edits suppress stale chunks immediately even if `updated_at` is
not changed. Re-ingest to restore retrieval. MD5 is a pragmatic change detector,
not a tamper-proof signature; database administrators are trusted. ACL changes
require no re-ingestion. Windows count Unicode code points, not grapheme clusters,
and may split words or combining sequences at boundaries.

## Setup prerequisites

Go 1.27.1+, Docker Compose v2, running Linux Docker engine, and PowerShell 7 for
these commands. Run from the repository root. Initial downloads require network
access. Integration tests require localhost HTTP sockets and PostgreSQL access.
Go does not load `.env` automatically. Existing Phase 2 keys can be reused.
Authentication remains development-only. Database credentials are trusted operator
credentials and must not be exposed as an end-user ingestion service.

## Ordered validation commands and expected results

### 1. Start the database and inspect schema

```powershell
go version
docker compose config --quiet
docker compose up -d --wait
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT to_regclass('public.roles'),to_regclass('public.chunks');"
```

Expect a healthy container. A fresh volume applies 001, 002, 003 automatically.
For an existing database with roles but no chunks, apply 003 once:

```powershell
Get-Content -Raw migrations/003_retrieval.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
if ($LASTEXITCODE -ne 0) { throw 'Migration 003 failed; inspect before retrying' }
```

Expect `COMMIT`. Apply missing 001/002 first for older databases following
[seed notes](../seed/README.md). Do not reapply existing migrations or delete volumes.
There is no automatic migration runner. `/readyz` checks connectivity, not schema.

### 2. Ingest and confirm repeatability

```powershell
go run ./cmd/ingest --tenant acme
go run ./cmd/ingest --tenant acme
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT d.title,count(c.id) FROM documents d JOIN tenants t ON t.id=d.tenant_id LEFT JOIN chunks c ON c.document_id=d.id WHERE t.slug='acme' GROUP BY d.id ORDER BY d.title;"
```

Each command reports `Ingested 4 documents using dev-hash-v1 (128 dimensions)`.
Each original demo document has one chunk after either run. Replacements change
chunk IDs. No source IDs, grants or user records are changed. A customized corpus
will have different counts. Set `DATABASE_URL` explicitly when targeting another DB.

### 3. Run automated verification

```powershell
gofmt -l cmd internal
go vet ./...
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 -v ./...
git diff --check
```

Expect no formatting/vet errors and all tests passing. Confirm
`TestSearchIntegration`, `TestDocumentsIntegration`, and `TestUsersIntegration`
actually execute rather than skip. The database must contain the unmodified seeded
permission matrix. Search integration creates/removes isolated tenant fixtures and
re-ingests the existing acme documents; it does not change seeded content or ACLs.
Without `TEST_DATABASE_URL`, SQL tests skip and do not validate retrieval policy.

### 4. Exercise the live tools

```powershell
# Generate keys only when neither key already exists:
go run ./cmd/devtoken keygen
go run ./cmd/server
```

Follow the exact [README](../README.md) token and search examples in another shell.
Search `salary` with limit 20, changing only bearer tokens: Alice/Bob/Carol/Dave
receive 2/2/4/1 original seeded chunks. Alice never receives Salary Guidelines,
Bob never receives Engineering Roadmap, and Dave sees only Company Handbook.
Try limit 1: a forbidden exact match must not eliminate an allowed lower-ranked
result. Extra identity/role/tenant fields, empty/missing query, overlong query and
out-of-range limits must fail. Repeat the Phase 2 direct-read denial example after
search; a returned chunk cannot grant access to another document.

## Automated test coverage

- `TestHashAndChunk`: deterministic query/document compatibility; finite/nonzero
  vector checks; cancellation; Unicode validity, exact boundary, empty text,
  100-character overlap and lossless reconstruction.
- `TestRetrieverBoundary`: invalid principals/UTF-8/blank/overlong queries/bounds
  never reach storage; only server principal IDs and model configuration are passed;
  repeated calls hit the repository rather than a policy cache.
- `TestHTTPDocumentBoundary`: both `2026-07-28` and `2025-11-25`, Alice/Bob/Alice
  caller switching, search success and `results: []`, strict required/type/length/
  range inputs and identity spoofing rejection alongside direct-read tests.
- `TestSearchIntegration`: forbidden exact match versus allowed top-1 fallback;
  no-ACL default denial; actual role membership and direct user ACLs; membership/
  ACL revocation; inactive and foreign principals; repeat ingestion without duplicate
  chunks; rollback after replacement begins with an invalid vector; provider failure;
  stale chunk suppression/re-ingestion; incompatible model/dimension exclusion;
  real four-caller matrix. Existing authentication/authorization suites remain active.

## Validation record — 2026-10-20

Executed migration 003 against the existing healthy Phase 2 Compose database;
the transaction committed without altering documents or ACLs. Ran the ingestion
CLI successfully against acme: four documents, `dev-hash-v1`, 128 dimensions.
Formatting and `go vet ./...` passed. The uncached complete test suite passed
with `TEST_DATABASE_URL` targeting the development database, including all three
PostgreSQL suites and both SDK protocol revisions. The initial sandboxed test
attempt could not open localhost sockets; the authorized rerun passed.

Also applied 001, 002 and 003 in order to the dedicated temporary database
`phase3_validation_20261020`; all three committed. The final uncached verbose suite
passed against that fresh database, including the added rollback-after-delete,
ACL revocation and incompatible-model/dimension checks. The temporary database
was removed afterward; the upgraded development database and its four ingested
demo documents remain available. `gofmt -l cmd internal` and `git diff --check`
were clean (Git reports only its configured LF-to-CRLF conversion notices).

Manual commands above are reproducible instructions. This record does not claim
every manual variant or a standalone running-server HTTP smoke test was executed;
HTTP behavior is covered by SDK tests with a fake repository, and real SQL by
integration tests. No external model or host-driven prompt-injection demo was run.

## Troubleshooting

- `relation chunks does not exist`: apply migration 003 to the actual database
  selected by `DATABASE_URL`; restarting Compose does not upgrade an existing volume.
- Empty search with populated sources: run ingestion, check content hash/model,
  tenant, active status and current ACL/membership. Listing does not require chunks.
- Search after a content edit returns no hit: stale chunks are intentionally hidden;
  rerun ingestion. ACL edits do not require regeneration.
- Generic embedding/ingestion failure: inspect operator-side schema and text;
  punctuation-only windows have no vector. Previously committed document chunks
  survive a failed replacement; changed source content still hides those old chunks.
- Unexpected relevance: the provider hashes words and has collisions, with no
  threshold or language understanding. Do not interpret ranking as policy failure.
- Docker pipe errors: start Docker Desktop with the Linux engine. Restricted runs
  need approval for Docker and localhost sockets. Skipped SQL tests are not evidence
  of correct database policy.

## Limitations and deferred work

Small local corpora only: exact scan and materialization, hash calculation during
search, in-memory document chunking, sequential ingestion, and per-document locks.
No concurrency/load benchmarks or ingestion memory limit. No ANN index, real semantic
model, embedding API, manifest/file importer, source parser, generation, pagination,
or relevance threshold. A document can produce multiple hits; the tool does not
deduplicate by source. No chunk ACLs: document grants apply to all its chunks.
Persistent audit storage and the complete prompt-injection demo remain Phase 4;
production OAuth, RLS and provider migration remain later work.
