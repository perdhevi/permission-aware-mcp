# permission-aware-mcp

A Go MCP server demonstrating authentication and authorization for permission-aware
retrieval. **The LLM cannot select its own user identity or role.** Caller identity
comes exclusively from a verified bearer token; the database supplies tenant membership.

Implemented through Phase 2: RS256 development authentication, PostgreSQL identity
and role lookup, tenant isolation, document ACLs, `whoami`,
`list_accessible_sources`, and `get_document`. Semantic retrieval follows in Phase 3.
The broader roadmap is preserved in [PROJECT_SPEC.md](PROJECT_SPEC.md).

## Phase implementation guides

Each implementation phase has a detailed change record and reproducible validation
guide. Keep these documents updated as the corresponding implementation changes:

- [Phase 00 — project skeleton and database](docs/phase-00.md)
- [Phase 01 — authentication and whoami](docs/phase-01.md)
- [Phase 02 — roles and document authorization](docs/phase-02.md)

## Requirements and setup

Use Go 1.27.1 or later and Docker Compose v2 with a running Docker engine.
Run commands from the repository root:

```sh
docker compose up -d --wait
go run ./cmd/devtoken keygen
go run ./cmd/devtoken mint --subject alice
go run ./cmd/server
```

The server listens at `http://127.0.0.1:8080/mcp`. Health endpoints are
`GET /healthz` (process) and `GET /readyz` (database connectivity).
Compose binds PostgreSQL to localhost:5432 and persists data in a named volume.
On first initialization it applies both SQL migrations, enables pgvector, and seeds
tenant `acme`, active users `alice`, `bob`, `carol`, `dave`, roles and four documents.
**Existing phase 1 databases need migration 002 before starting the updated server.**
Migrations do not rerun on an existing volume. See [seed notes](seed/README.md)
for the explicit upgrade command; preserve your database volume.

Configuration uses environment variables; `.env.example` lists the development
defaults. The Go commands do **not** automatically load `.env` files. Export values
in your shell when overriding defaults. `APP_ENV` values other than `development`
and authentication modes other than `dev` fail startup. This server is not a
production OAuth deployment.

Key generation refuses to overwrite either existing key. Keys and `.env` are
gitignored. Private keys use mode 0600 and the directory uses 0700 on systems
that support Unix modes; on Windows restrict the directory with filesystem ACLs.
Minting prints only the JWT to stdout, defaults to one hour and supports
`--ttl 15m` and `--key <private.pem>`. `keygen --dir <directory>` selects a different
directory; set `DEV_JWT_PUBLIC_KEY_FILE` and mint's `--key` accordingly.
Issuer, audience and required scope use the same environment configuration in both commands.
Changing the issuer also requires corresponding database identities.

## Call whoami

Configure an MCP client for Streamable HTTP, the endpoint above and the header
`Authorization: Bearer <minted-token>` on **every** request. The official Go SDK
is pinned to v1.8.0. The transport is stateless, which supports MCP `2026-07-28`
and legacy clients; no authenticated principal is stored across HTTP requests.
Call `whoami` with `{}`. Unexpected arguments are rejected by its schema.

PowerShell example, in a second terminal while the server runs:

```powershell
$token = go run ./cmd/devtoken mint --subject alice
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

Expect the seeded user's UUID, subject `alice`, tenant `acme`, and scope
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
revocation, cross-tenant constraints, and Unicode content limits.
Database tests skip unless `TEST_DATABASE_URL` is set. Run them against the local
Compose database with PowerShell:

```powershell
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 ./...
```

Integration tests check real seeded users, pgvector and the permission matrix,
and remove their dedicated fixtures without changing seeded users.

## Read authorized documents

Use the same `$body` and `$headers` from the PowerShell example above:

```powershell
$headers['Mcp-Name'] = 'list_accessible_sources'
$request = $body | ConvertFrom-Json -AsHashtable
$request.params.name = 'list_accessible_sources'
$request.params.arguments = @{ limit = 20 }
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

This example requires PowerShell 7. Listing defaults to 20, accepts 1–100, and
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

## Architecture and next phase

JWT validation → identity → database principal → MCP tool → authorization service
→ tenant/ACL-filtered repository query.
Tools contain no SQL. Every HTTP request revalidates the token and database user.
`auth.TokenInfo.UserID` contains the local UUID; the principal is carried in SDK
token information, never tool arguments or client metadata.

See [architecture](docs/architecture.md), [security](docs/security.md) and
[demo](docs/demo.md). Phase 3 adds permission-filtered semantic retrieval and
ingestion. Embeddings, search, persistent audit storage and production OAuth
remain deferred.
