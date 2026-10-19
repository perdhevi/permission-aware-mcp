# permission-aware-mcp

A Go MCP server demonstrating the authentication foundation for permission-aware
retrieval. **The LLM cannot select its own user identity or role.** Caller identity
comes exclusively from a verified bearer token; the database supplies tenant membership.

This pass implements Phase 0–1: project skeleton, RS256 development authentication,
PostgreSQL identity lookup and `whoami`. It deliberately stops before roles, ACLs or RAG.
The broader roadmap is preserved in [PROJECT_SPEC.md](PROJECT_SPEC.md); the limited
scope above governs this implementation.

## Phase implementation guides

Each implementation phase has a detailed change record and reproducible validation
guide. Keep these documents updated as the corresponding implementation changes:

- [Phase 00 — project skeleton and database](docs/phase-00.md)
- [Phase 01 — authentication and whoami](docs/phase-01.md)

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
On first initialization it applies `migrations/001_initial.sql`, enables pgvector,
and seeds tenant `acme` and active users `alice`, `bob`, `carol`, `dave`.
Migrations do not rerun on an existing database volume. See [seed notes](seed/README.md).

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
`knowledge:read`. No token or signing data is returned. Omitting the bearer token
or using an invalid token returns HTTP 401. Unknown and inactive users also receive
401. JWTs without the configured scope fail authentication with 401; this verifier
checks scope before the SDK middleware's additional scope check.

## Verification

```sh
go vet ./...
go test ./...
```

Tests exercise RS256 signature and claim validation, user resolution, inactive-user
rejection, HTTP authentication failures, strict tool arguments, and alternating
Alice/Bob callers through the official SDK client using both protocol revisions.
Database tests skip unless `TEST_DATABASE_URL` is set. Run them against the local
Compose database with PowerShell:

```powershell
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 ./...
```

Integration tests check real seeded users and pgvector, and create then remove
a dedicated inactive test identity without changing seeded users.

## Architecture and next phase

JWT validation → identity → repository lookup → principal → MCP tool.
Tools contain no SQL. Every HTTP request revalidates the token and database user.
`auth.TokenInfo.UserID` contains the local UUID; the principal is carried in SDK
token information, never tool arguments or client metadata.

See [architecture](docs/architecture.md), [security](docs/security.md) and
[demo](docs/demo.md). The next phase is database roles, document ACLs and independently
authorized `list_accessible_sources` / `get_document`. Permission-filtered semantic
retrieval follows that phase. No embedding provider, document data or fake policy
implementation is included here.
