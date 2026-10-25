# permission-aware-mcp

A Go MCP server demonstrating authentication and authorization for permission-aware
retrieval. **The LLM cannot select its own user identity or role.** Caller identity
comes exclusively from a verified bearer token; the database supplies tenant membership.

Implemented through Phase 5: offline RS256 development authentication (the default
demo), optional Keycloak OIDC/OAuth, PostgreSQL identity
and role lookup, tenant isolation, document ACLs, `whoami`,
`list_accessible_sources`, `get_document`, and permission-filtered `search_knowledge`.
An offline hash embedder and ingestion CLI provide reproducible pgvector retrieval.
Persistent access audits and a prompt-injection demo verify that hostile retrieved
instructions cannot grant access to confidential documents.

## Phase implementation guides

Each implementation phase has a detailed change record and reproducible validation
guide. Keep these documents updated as the corresponding implementation changes:

- [Phase 00 — project skeleton and database](docs/phase-00.md)
- [Phase 01 — authentication and whoami](docs/phase-01.md)
- [Phase 02 — roles and document authorization](docs/phase-02.md)
- [Phase 03 — ingestion and permission-filtered retrieval](docs/phase-03.md)
- [Phase 04 — audit and security demo](docs/phase-04.md)
- [Phase 05 — Keycloak OIDC/OAuth and discovery](docs/phase-05.md)

The [topic isolation PoC](docs/topic-isolation-poc.md) (same query, different
documents per user) is an optional demo fixture, not an implementation phase.

## Requirements and setup

Use Go 1.27.1 or later and Docker Compose v2 with a running Docker engine.
Run commands from the repository root:

The default demo is fully offline and needs no identity provider. Leave
`AUTH_MODE=dev` (the default):

```sh
docker compose up -d --wait
go run ./cmd/ingest --tenant acme
go run ./cmd/devtoken keygen
go run ./cmd/server
```

In a second terminal, mint a token (one hour by default) for a seeded user with
`go run ./cmd/devtoken mint --subject alice` (or `bob`, `carol`, `dave`) and use it
with the request examples below. Keycloak is not started by default.

The server listens at `http://127.0.0.1:8080/mcp`. Health endpoints are
`GET /healthz` (process) and `GET /readyz` (database connectivity).
Compose binds PostgreSQL to localhost:5432 and persists data in a named volume.
On first initialization it applies all five SQL migrations, enables pgvector, and seeds
tenant `acme`, active users `alice`, `bob`, `carol`, `dave`, roles and four documents.
**Existing databases need missing migrations in order (002, 003, 004, 005).**
Migrations do not rerun on an existing volume. See [seed notes](seed/README.md)
for the explicit upgrade command; preserve your database volume.

Configuration uses environment variables; `.env.example` lists the development
defaults. The Go commands do **not** automatically load `.env` files. Export values
in your shell when overriding defaults. `APP_ENV` values other than `development`
and authentication modes other than `dev` or `oidc` fail startup. This remains
a local development deployment.

Key generation refuses to overwrite either existing key. Keys and `.env` are
gitignored. Private keys use mode 0600 and the directory uses 0700 on systems
that support Unix modes; on Windows restrict the directory with filesystem ACLs.
Minting prints only the JWT to stdout, defaults to one hour and supports
`--ttl 15m` and `--key <private.pem>`. `keygen --dir <directory>` selects a different
directory; set `DEV_JWT_PUBLIC_KEY_FILE` and mint's `--key` accordingly.
Issuer, audience and required scope use the same environment configuration in both commands.
Changing the issuer also requires corresponding database identities.

### Optional: Keycloak OIDC/OAuth login

The retrieval, authorization and audit behavior is identical in both modes; only
the token issuer changes. To use a real external authorization server with browser
authorization code and PKCE, follow the
[Phase 05 setup](docs/phase-05.md#ordered-validation-commands-and-expected-results):
start the optional `oidc` Compose profile, load local OIDC users, export the four
authentication settings in both terminals, and run the server. Then run
`$token = go run ./cmd/login`, open the printed URL, and sign in as `alice` with the
local fixture password `local-demo-only`. The command prints only the access token
to stdout; browser instructions go to stderr. No local signing keys are needed.
Login needs free local ports 8081 (Keycloak) and 8765 (CLI callback).
OIDC uses discovery/JWKS from the configured issuer, validates resource audience and
required scope, and exposes Protected Resource Metadata in the 401 challenge. See
Phase 05 for the provider profile and limitations.

## Call whoami

Configure an MCP client for Streamable HTTP, the endpoint above and the header
`Authorization: Bearer <minted-token>` on **every** request. The official Go SDK
is pinned to v1.8.0. The transport is stateless, which supports MCP `2026-07-28`
and legacy clients; no authenticated principal is stored across HTTP requests.
Call `whoami` with `{}`. Unexpected arguments are rejected by its schema.

PowerShell example, in a second terminal while the server runs:

```powershell
$token = go run ./cmd/devtoken mint --subject alice
# Optional OIDC mode instead: $token = go run ./cmd/login (open the printed URL, sign in as alice)
$headers = @{
  Authorization = "Bearer $token"
  Accept = 'application/json, text/event-stream'
  'MCP-Protocol-Version' = '2026-07-28'
  'Mcp-Method' = 'tools/call'
  'Mcp-Name' = 'whoami'
}
$body = @{
  jsonrpc = '2.0'
  id = 1
  method = 'tools/call'
  params = @{
    name = 'whoami'
    arguments = @{}
    _meta = @{
      'io.modelcontextprotocol/protocolVersion' = '2026-07-28'
      'io.modelcontextprotocol/clientInfo' = @{ name = 'powershell-demo'; version = '1' }
      'io.modelcontextprotocol/clientCapabilities' = @{}
    }
  }
} | ConvertTo-Json -Depth 8
$reply = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
$reply.result.structuredContent
```

Expect the seeded user's UUID, subject `alice` (a fixed UUID in OIDC mode), tenant `acme`, and scope
`knowledge:read`, and database role `engineering`. No token or signing data is returned. Omitting the bearer token
or using an invalid token returns HTTP 401. Unknown and inactive users also receive
401. JWTs without the configured scope fail authentication with 401; this verifier
checks scope before the SDK middleware's additional scope check.

## Verification

```sh
go vet ./...
go test ./...
```

Tests exercise authentication, database roles, strict tool arguments, caller
isolation through both SDK protocol revisions, authorized listing/direct reads,
permission-filtered ranking, ingestion, revocation, cross-tenant constraints,
and Unicode content limits.
Database tests skip unless `TEST_DATABASE_URL` is set. Run them against the local
Compose database with PowerShell:

```powershell
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 ./...
```

Integration tests check real seeded users, pgvector and the permission matrix,
and remove their dedicated fixtures without changing seeded users.

## Read authorized documents

Use the same `$headers` from the PowerShell example above:

```powershell
$headers['Mcp-Name'] = 'list_accessible_sources'
$request = @{
  jsonrpc = '2.0'
  id = 1
  method = 'tools/call'
  params = @{
    name = 'list_accessible_sources'
    arguments = @{ limit = 20 }
    _meta = @{
      'io.modelcontextprotocol/protocolVersion' = '2026-07-28'
      'io.modelcontextprotocol/clientInfo' = @{ name = 'powershell-demo'; version = '1' }
      'io.modelcontextprotocol/clientCapabilities' = @{}
    }
  }
}
$body = $request | ConvertTo-Json -Depth 8
$sources = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
$sources.result.structuredContent.sources

$documentID = $sources.result.structuredContent.sources[0].document_id
$headers['Mcp-Name'] = 'get_document'
$request.params.name = 'get_document'
$request.params.arguments = @{ document_id = $documentID; max_chars = 12000 }
$body = $request | ConvertTo-Json -Depth 8
$document = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
$document.result.structuredContent
```

This example runs on Windows PowerShell 5.1 and PowerShell 7. Listing defaults to 20, accepts 1–100, and
returns only document UUIDs and titles in title/UUID order. Direct reads accept
a UUID and default to 12000 Unicode characters (1–50000), returning a `truncated`
flag. Missing and forbidden documents both produce the MCP tool error
`resource not found`. Every call rechecks current database permissions.

| Caller | Role | Accessible documents |
| --- | --- | --- |
| Alice | engineering | Company Handbook, Engineering Roadmap |
| Bob | hr | Company Handbook, Salary Guidelines |
| Carol | executive | All four |
| Dave | employee | Company Handbook |

Roles have no implicit hierarchy or global bypass. The seed explicitly grants
Company Handbook to all four roles and each confidential document to its readers.

## Search authorized knowledge

After migration 003, run `go run ./cmd/ingest --tenant acme` to replace chunks for
the existing database documents. This trusted CLI uses database credentials and
preserves document IDs and ACLs. Repeat it after content changes. It is not an MCP tool.
Using the request variables from the examples above:

```powershell
$headers['Mcp-Name'] = 'search_knowledge'
$request.params.name = 'search_knowledge'
$request.params.arguments = @{ query = 'deployment automation'; limit = 5 }
$body = $request | ConvertTo-Json -Depth 8
$search = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
$search.result.structuredContent.results
```

Search requires a nonblank query of at most 4000 Unicode characters. Limit defaults
to 5 (1–20). Results contain chunk/document UUIDs, title, content and cosine distance
(lower is closer). Tenant and current ACLs filter candidates **before** exact ranking.
No count or unauthorized metadata is returned. With `limit = 20`, the seeded corpus
returns 2/2/4/1 chunks for Alice/Bob/Carol/Dave; Alice never receives Salary Guidelines.

The fixed `dev-hash-v1` embedder uses 128 normalized word-hash buckets. It needs no
API keys or external service, but has lexical overlap and hash collisions rather
than language understanding. There is no relevance threshold, so unrelated authorized
chunks may appear. Retrieved text is untrusted data. See the [Phase 03 guide](docs/phase-03.md)
for ingestion transactions, stale-content handling and full validation instructions.

## Try it: same question, different documents per user

This walkthrough shows the core guarantee from the caller's side. Four users ask
the same question about one topic, "Project Falcon". Each user gets a different set
of documents, and a document granted to one user stays invisible to everyone else,
even when they know its ID.

An optional fixture adds four Falcon documents. Their content all matches the same
query, so any difference in results comes from permissions, not relevance:

| Document | Who can read it | Granted by |
| --- | --- | --- |
| Project Falcon Overview | everyone | all four roles |
| Project Falcon Technical Design | Alice only | user grant |
| Project Falcon Staffing Plan | Bob only | user grant |
| Project Falcon Budget Approval | Carol only | user grant |

The confidential documents are granted to a single *user*, not a role. Carol's
`executive` role therefore does not reach Alice's or Bob's documents: there is no
role hierarchy or admin bypass.

### 1. Load the fixture

With the stack from [Requirements and setup](#requirements-and-setup) running and
the server started with `AUTH_MODE=dev`, run from the repository root:

```powershell
Get-Content -Raw seed/topic-isolation.sql | docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
go run ./cmd/ingest --tenant acme
```

Expect `Ingested 8 documents using dev-hash-v1 (128 dimensions)`.

### 2. Define two helpers

`Use-Caller` mints a development token for a user and keeps it off screen.
`Invoke-Mcp` sends one MCP `tools/call` as that user. The helpers work in Windows
PowerShell 5.1 and PowerShell 7.

```powershell
function Use-Caller([string]$Subject) {
    $global:McpToken = go run ./cmd/devtoken mint --subject $Subject
    Write-Host "Now calling as $Subject" -ForegroundColor Cyan
}
function Invoke-Mcp([string]$Tool, [hashtable]$Arguments = @{}) {
    $headers = @{ Authorization = "Bearer $global:McpToken"; Accept = 'application/json, text/event-stream'
                  'MCP-Protocol-Version' = '2026-07-28'; 'Mcp-Method' = 'tools/call'; 'Mcp-Name' = $Tool }
    $body = @{ jsonrpc = '2.0'; id = 1; method = 'tools/call'
               params = @{ name = $Tool; arguments = $Arguments
                           _meta = @{ 'io.modelcontextprotocol/protocolVersion' = '2026-07-28'
                                      'io.modelcontextprotocol/clientInfo' = @{ name = 'manual'; version = '1' }
                                      'io.modelcontextprotocol/clientCapabilities' = @{} } } } | ConvertTo-Json -Depth 8
    $reply = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
    if ($reply.result.isError) { Write-Host "TOOL ERROR: $($reply.result.content.text -join ' ')" -ForegroundColor Red }
    else { $reply.result.structuredContent }
}
```

### 3. Alice asks about Project Falcon

```powershell
Use-Caller alice
Invoke-Mcp whoami | Format-List subject, roles
(Invoke-Mcp search_knowledge @{ query = 'Project Falcon migration'; limit = 10 }).results | Format-Table title, distance
$design = ((Invoke-Mcp list_accessible_sources).sources | Where-Object title -eq 'Project Falcon Technical Design').document_id
Invoke-Mcp get_document @{ document_id = $design } | Format-List title, content
```

Alice's Falcon results are **Overview** and **Technical Design**. Her other documents
also appear, ranked lower. She reads the Technical Design, and `$design` keeps its ID.

### 4. Bob asks the same question, then tries Alice's document

```powershell
Use-Caller bob
(Invoke-Mcp search_knowledge @{ query = 'Project Falcon migration'; limit = 10 }).results | Format-Table title, distance
Invoke-Mcp get_document @{ document_id = $design }
Invoke-Mcp get_document @{ document_id = [guid]::NewGuid().ToString() }
```

Bob's Falcon results are **Overview** and **Staffing Plan**, with no Technical Design.
Reading Alice's document by its real ID returns `TOOL ERROR: resource not found`,
the same answer as for a random, nonexistent ID. Bob cannot even learn that the
document exists.

### 5. Carol and Dave

```powershell
Use-Caller carol
(Invoke-Mcp search_knowledge @{ query = 'Project Falcon migration'; limit = 10 }).results | Format-Table title, distance
Invoke-Mcp get_document @{ document_id = $design }

Use-Caller dave
(Invoke-Mcp search_knowledge @{ query = 'Project Falcon migration'; limit = 10 }).results | Format-Table title, distance
```

Carol gets **Overview** and **Budget Approval**, and she is denied Alice's document
even as an executive. Dave gets the **Overview** only.

| Caller | Role | Falcon documents returned for the same query |
| --- | --- | --- |
| Alice | engineering | Overview, Technical Design |
| Bob | hr | Overview, Staffing Plan |
| Carol | executive | Overview, Budget Approval |
| Dave | employee | Overview |

To try other pairs, take the ID of Bob's Staffing Plan or Carol's Budget Approval as
its owner, then switch callers and read it. `Invoke-Mcp list_accessible_sources`
shows each caller's full document list.

### 6. Check who received what

Every successful call records the document IDs it returned in the audit log. As the
operator, ask which users have received each Falcon document:

```powershell
"SELECT DISTINCT d.title, u.subject FROM audit_log a JOIN users u ON u.id = a.user_id JOIN documents d ON d.id = ANY(a.disclosed_document_ids) WHERE d.source LIKE 'falcon-%' ORDER BY 1, 2;" | docker compose exec -T postgres psql -X -U permission_mcp -d permission_mcp
```

Each confidential document lists only its owner. The Overview lists every caller
who searched.

### 7. Automated check and cleanup

`./seed/topic-isolation-demo.ps1` runs the same comparison for all four callers,
cross-checks direct reads of every Falcon document, and prints `PASS` or `FAIL`.
The repository integration tests expect the original four-document matrix, so
remove the fixture before running them:

```powershell
Get-Content -Raw seed/topic-isolation-cleanup.sql | docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
```

Design notes, the validation record and limitations are in the [topic isolation PoC guide](docs/topic-isolation-poc.md).

## Architecture and next phase

JWT validation → identity → database principal → MCP tool → authorization service
→ tenant/ACL-filtered repository query.
Tools contain no SQL. Every HTTP request revalidates the token and database user.
`auth.TokenInfo.UserID` contains the local UUID; the principal is carried in SDK
token information, never tool arguments or client metadata.

Every authenticated tool call, including schema-rejected spoofing, writes an
`audit_log` record before returning results. An audit write failure returns only
`resource service unavailable`. Records contain server-generated request IDs,
database caller/tenant IDs, tool/action, requested document UUID, ALLOW/DENY/ERROR,
fixed reason, SHA-256 query hash, read duration, and the distinct document UUIDs each
successful list, search or direct read returned, so operators can answer which callers
received a given document. No raw query, title or document text is stored. Audit access is operator-only; no audit MCP tool is exposed.

For the optional hostile engineering-document fixture and reproducible host demo,
see [Phase 04](docs/phase-04.md) and [demo](docs/demo.md). Existing ACLs still deny
Alice's attempt to read Salary Guidelines, and that denial is persisted.
See [architecture](docs/architecture.md) and [security](docs/security.md).
Phase 5 adds Keycloak discovery, JWT access-token verification, resource metadata
and browser PKCE login; see its guide above. Phase 6 defense-in-depth work and
language-model embedding providers remain deferred.

## License

[MIT](LICENSE)
