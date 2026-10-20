# Phase 01 — Authentication and whoami

## Scope and outcome

This phase adds the first authenticated MCP path: verify a development bearer JWT,
resolve an active local database user, construct a principal, and return that
identity through `whoami`. It builds on [Phase 00](phase-00.md) and deliberately
stops before role-based or document-level authorization.

The security boundary is:

```text
Authorization: Bearer <JWT>
          |
          v
RS256 signature + issuer + audience + expiry + subject + scope
          |
          v
Identity (verified issuer and subject)
          |
          v
UserRepository lookup by issuer + subject
          |
          v
Principal (local user UUID, database tenant, verified scopes)
          |
          v
SDK TokenInfo -> whoami -> structured response
```

MCP arguments do not participate in creating the principal. A signed token for an
unknown or inactive local user still fails authentication.

## Changes by component

| Files | Change and purpose |
| --- | --- |
| `internal/authn/identity.go` | Introduce `Identity`, `Principal`, `ErrUnauthenticated`, and the injected repository-based identity resolver. Copy scopes and reject missing/inactive local users. |
| `internal/authn/verifier.go` | Define `TokenVerifier`, allowing a future authentication implementation without changing tool handlers. |
| `internal/authn/devjwt.go` | Load the configured RSA public key, pin RS256, validate required claims and exact scope membership, and return verified identity. Discard raw JWT parser errors at the authentication boundary. |
| `cmd/devtoken/main.go` | Add `keygen` and `mint`, exclusive key-file creation, configurable key paths, a required subject, and a positive token lifetime. |
| `internal/mcpserver/server.go` | Use official SDK bearer middleware and stateless Streamable HTTP at `/mcp`; resolve a fresh principal per HTTP request and populate SDK `TokenInfo`. |
| `internal/mcpserver/tools/whoami.go` | Register the only tool, with an empty-object input schema and `additionalProperties: false`; read principal exclusively from SDK token information. |
| `internal/authn/*_test.go` | Test JWT validation and repository-based principal resolution. |
| `internal/mcpserver/server_test.go` | Exercise HTTP authentication and tool identity boundaries using the official SDK client for current and legacy protocol versions. |
| `cmd/devtoken/main_test.go` | Verify generated PEM keys parse and repeat key generation cannot overwrite them. |
| `docs/security.md`, `docs/architecture.md`, `docs/demo.md`, `README.md` | Explain trust boundaries, exact client requests, expected behavior, and deferred authorization/retrieval. |

## Design decisions

The official Go SDK v1.8.0 handles MCP, schemas, HTTP transport, and bearer
middleware. This project does not implement its own JSON-RPC stack. Stateless
Streamable HTTP supports target protocol `2026-07-28`; the SDK also supports legacy
`2025-11-25` clients. Each HTTP request revalidates its bearer token and reloads the
local user rather than retaining a principal in a session or server closure.

`auth.TokenInfo.UserID` holds the local database UUID. `TokenInfo.Extra` carries
the resolved principal, which `whoami` reads through `req.Extra.TokenInfo`. Client
arguments and client `_meta` are separate from this server-created token information.

The verifier requires issuer, audience, expiration, a nonblank subject, a matching
signature and the exact configured scope. A present `nbf` claim is also checked.
An RSA public key must be at least 2048 bits. Missing scope is rejected by the
verifier with HTTP 401; the official middleware additionally checks scopes and expiry.

Unknown/inactive users return generic authentication failure. Repository outages
return a generic HTTP 500 rather than authentication success or database details.
Token parser errors, raw tokens and Authorization headers are not logged. HTTP
requests receive generated request IDs; successful tool logs include request ID,
tool, local user ID, tenant ID and duration.

## Prerequisites

Complete Phase 00's Docker, migration, seed and configuration checks first. Use
PowerShell 7 for the examples below; `-SkipHttpErrorCheck` is used to inspect
expected HTTP failures. Run Go commands from the repository root.

These instructions use development defaults. If changing issuer, audience or scope,
keep server and minting environments consistent. A changed issuer must also have
matching local database user records.

## Validation steps

### 1. Create or reuse development keys

For first-time setup:

```powershell
go run ./cmd/devtoken keygen
Test-Path .local/keys/private.pem
Test-Path .local/keys/public.pem
```

Expect both paths to exist. If keys already exist, skip generation and reuse them.
Repeating `keygen` should fail without changing the keys. Keys are gitignored under
`.local/`; private keys must remain private. Windows key-directory permissions
must be controlled with filesystem ACLs because Unix file modes do not enforce them.

### 2. Start the server

```powershell
docker compose up -d --wait
go run ./cmd/server
```

Expect `server listening` at `127.0.0.1:8080`. Leave that terminal running. Use a
second terminal for the remaining steps.

### 3. Mint Alice's token and construct a current-protocol request

```powershell
$token = go run ./cmd/devtoken mint --subject alice
if ($LASTEXITCODE -ne 0) { throw 'Token minting failed' }
$headers = @{
  Authorization = "Bearer $token"
  Accept = 'application/json, text/event-stream'
  'MCP-Protocol-Version' = '2026-07-28'
  'Mcp-Method' = 'tools/call'
  'Mcp-Name' = 'whoami'
}
$request = @{
  jsonrpc = '2.0'
  id = 1
  method = 'tools/call'
  params = @{
    name = 'whoami'
    arguments = @{}
    _meta = @{
      'io.modelcontextprotocol/protocolVersion' = '2026-07-28'
      'io.modelcontextprotocol/clientInfo' = @{ name = 'phase-01-validation'; version = '1' }
      'io.modelcontextprotocol/clientCapabilities' = @{}
    }
  }
}
$body = $request | ConvertTo-Json -Depth 8
$reply = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
$reply.result.structuredContent
```

Expect `subject: alice`, `tenant: acme`, `scopes: [knowledge:read]`, and Alice's
generated local UUID as `user_id`. The response must not contain a JWT, private
claims, key metadata, or the Authorization header. Default token lifetime is one
hour; `mint --ttl 15m` changes it. No tenant or role selection is included in tokens.

The method/name headers and client metadata above are required for this direct
current-protocol HTTP example. The official SDK client supplies protocol details
automatically; its behavior is exercised by the automated tests.

### 4. Verify caller changes require a different token

Keep `$body` unchanged and replace only the bearer token:

```powershell
$bobToken = go run ./cmd/devtoken mint --subject bob
$headers.Authorization = "Bearer $bobToken"
$bobReply = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
$bobReply.result.structuredContent
$headers.Authorization = "Bearer $token"
```

Expect subject `bob`, Bob's UUID, and tenant `acme`. The final assignment restores
Alice's token for the following checks. The tests also alternate Alice/Bob/Alice
against one server with distinct mocked tenants, helping detect identity leakage.

### 5. Verify missing, invalid, unknown-user and expired tokens fail

Reuse the valid request body so protocol validation cannot mask authentication results:

```powershell
$unknownToken = go run ./cmd/devtoken mint --subject unregistered-phase-01-user
$shortToken = go run ./cmd/devtoken mint --subject alice --ttl 1s
Start-Sleep -Seconds 2
$rejectionHeaders = @{}
foreach ($entry in $headers.GetEnumerator()) { $rejectionHeaders[$entry.Key] = $entry.Value }
$rejectionHeaders.Remove('Authorization')
$cases = @(
  @{ Name = 'missing'; Token = $null },
  @{ Name = 'malformed'; Token = 'invalid' },
  @{ Name = 'unknown user'; Token = $unknownToken },
  @{ Name = 'expired'; Token = $shortToken }
)
foreach ($case in $cases) {
  $rejectionHeaders.Remove('Authorization')
  if ($case.Token) { $rejectionHeaders.Authorization = "Bearer $($case.Token)" }
  $response = Invoke-WebRequest -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $rejectionHeaders -ContentType application/json -Body $body -SkipHttpErrorCheck
  if ($response.StatusCode -ne 401) { throw "$($case.Name): expected 401, got $($response.StatusCode)" }
  "$($case.Name): HTTP 401"
}
```

All four cases should return HTTP 401. Wrong issuer, audience, signature and
missing-scope failures are covered by the targeted JWT tests below without
changing the local key pair or server configuration.

### 6. Verify arguments cannot impersonate another user

```powershell
$request.params.arguments = @{ user_id = 'carol'; tenant_id = 'other-tenant'; role = 'executive' }
$spoofBody = $request | ConvertTo-Json -Depth 8
$spoof = Invoke-WebRequest -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $spoofBody -SkipHttpErrorCheck
$spoofResult = $spoof.Content | ConvertFrom-Json
if (-not $spoofResult.error -and -not $spoofResult.result.isError) { throw 'Unexpected acceptance of identity arguments' }
$spoof.Content
$request.params.arguments = @{}
$body = $request | ConvertTo-Json -Depth 8
$again = Invoke-RestMethod -Uri http://127.0.0.1:8080/mcp -Method Post -Headers $headers -ContentType application/json -Body $body
if ($again.result.structuredContent.subject -ne 'alice') { throw 'Authenticated identity changed' }
```

Expect an MCP schema validation error for unexpected arguments. A tool/schema
error can be represented inside an HTTP response, so inspect the MCP result rather
than assuming it must be HTTP 401. The next valid empty-argument call must still
return Alice. No identity fields are defined in the tool input schema.

### 7. Run targeted and full automated checks

```powershell
go test -count=1 -v ./internal/authn
go test -count=1 -v ./internal/mcpserver ./cmd/devtoken ./internal/config
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
go test -count=1 -v ./...
go vet ./...
gofmt -l cmd internal
```

Expect all tests to pass, including `TestUsersIntegration` without a skip. Expect
no vet diagnostics and no filenames from `gofmt`. SDK HTTP tests need permission
to bind and connect to localhost; database tests need access to localhost:5432.

## Automated test coverage

| Test | Assertions |
| --- | --- |
| `TestDevJWT` | Valid RS256 token succeeds; expired/missing-expiration tokens, wrong issuer/audience, bad signature, missing scope, scope substring, missing/blank subject, future `nbf`, alternate signing algorithm and malformed tokens fail. |
| `TestResolve` | Exact issuer/subject reaches the repository; known users produce database principals; unknown/inactive users fail; database errors remain distinguishable internally. |
| `TestHTTPIdentityBoundary` | Missing/malformed tokens and unknown/inactive users receive 401. Official SDK clients can call `whoami` using both `2026-07-28` and `2025-11-25`; alternating callers retain their own user and tenant. Input schema rejects extra properties; identity-spoofing arguments fail. Logs are checked for bearer-token leakage. Health/readiness endpoints return 200 with a healthy fake dependency. |
| `TestKeygenRefusesOverwrite` | Both generated PEM keys parse; repeat generation fails and private-key bytes remain unchanged. |
| `TestUsersIntegration` | Real PostgreSQL external identity lookup, seeded tenant membership, unknown issuer/subject rejection and inactive-user resolution rejection. See Phase 00 for database setup. |
| `TestReadinessFailure`, `TestFailClosedConfiguration` | Unavailable dependencies and unsupported configuration fail as documented in Phase 00. |

The SDK HTTP test uses a fake user repository; the PostgreSQL integration test
uses the real repository. The live demonstration combines the CLI-issued token,
running server and real database. These checks cover complementary boundaries.

## Validation record and troubleshooting

During the initial implementation on 2026-10-19, the uncached full suite passed
with real database integration enabled, `go vet` passed, and formatting was clean.
The exact README PowerShell request was executed against the running server:
Alice's database UUID and tenant `acme` were returned, missing/invalid bearer
tokens returned 401, identity arguments were rejected, and health/readiness
returned 200. Keys were generated locally and remained under ignored `.local/`.
This records the completed implementation checks; it does not imply every example
in this expanded guide has been executed independently or that the server is running.

- `missing required Mcp-Method header` or `Mcp-Name` means the direct request lacks
  a required protocol header. Copy all headers from step 3.
- Missing client capabilities metadata means `_meta` is incomplete. Include
  protocol version, client info and client capabilities as shown.
- A valid-looking token returning 401 can be expired, signed by another key, use
  the wrong issuer/audience/scope, or map to an unknown/inactive database user.
  Use targeted tests to isolate validation behavior; never paste tokens into logs.
- A generic identity-service 500 usually indicates database lookup failed. Check
  the container, connectivity and initialized schema. Readiness checks connectivity
  only, so a missing schema can still produce 200 from `/readyz`.
- An existing-key error is expected on repeat `keygen`. Reuse the pair. Alternate
  directories use `keygen --dir`, mint's `--key`, and `DEV_JWT_PUBLIC_KEY_FILE`.

## Limits and next phase

Authentication is development-only. There is no external OAuth/OIDC issuer,
discovery, login, key rotation, token revocation, RBAC, document ACLs or RAG.
Deleting a signing key does not revoke tokens already signed with it; changing
the server's public key requires restart. Keep the development service local.

Phase 02 adds database roles, document records and ACLs, an independent
authorization service, and authorized `list_accessible_sources` / `get_document`
tools; see [Phase 02](phase-02.md). Current `whoami` also returns database roles
and current startup requires migration 002. The validation record above describes
the original phase 1 implementation. Semantic retrieval follows in Phase 3.
