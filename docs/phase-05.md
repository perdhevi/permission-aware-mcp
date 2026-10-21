# Phase 05 — Keycloak OIDC/OAuth and discovery

## Scope and outcome

Add a real external authorization server and browser authorization-code/PKCE flow
as an optional local demo mode. `AUTH_MODE=dev` remains the default, offline demo
(README leads with it since 2026-10-21 so the retrieval demo needs no identity
provider; no code changed). This departs from the original roadmap's goal of
making OIDC the normal demonstration path. Both
implement the same verifier interface and resolve `(issuer, subject)` against the
database on every request. All existing tenant, ACL, retrieval and audit boundaries
remain in force. This is a resource server, not an authorization server or token proxy.

## Changes by file/component

| Files | Change |
| --- | --- |
| `internal/authn/oidc.go` | Issuer discovery, JWKS verification/cache/rotation through coreos/go-oidc; RS256, audience, expiry, subject, scope and Keycloak Bearer profile checks; sanitized errors and bounded HTTP calls. |
| `internal/config/{config,config_test}.go` | OIDC mode and canonical resource URL validation; development-only deployment guard retained; no dev public key required in OIDC mode. |
| `cmd/server/main.go` | Select verifier and enable resource discovery for OIDC without loading local signing keys. |
| `internal/mcpserver/server.go` | SDK metadata handlers at root and `/mcp` well-known paths; SDK bearer challenge links to canonical metadata URL. |
| `cmd/login/main.go`, `cmd/login/main_test.go` | Three-minute browser login, random state, S256 PKCE, fixed loopback callback, response issuer validation when advertised, resource parameter, verified access token to stdout; sanitized failures and callback tests. |
| `cmd/devtoken/main.go`, `seed/prompt-injection-demo.ps1` | Prevent minting locally signed tokens while configured for OIDC; accept an explicit Alice OIDC token in the optional injection demo. |
| `docker-compose.yml`, `seed/keycloak-realm.json` | Optional `oidc` profile; pinned Keycloak 26.8.1, persisted realm, stable subject mapper, four demo users, pre-registered public PKCE client, explicit MCP audience/scope. |
| `seed/oidc-users.sql` | Optional repeatable OIDC identity/role fixture; preserve dev users, sources and ACLs. Not a schema migration. |
| OIDC, discovery and integration tests | Claim rejection, rotation/outages, config, metadata, and real Keycloak browser-form code flow through both MCP revisions. |
| `go.mod`, `go.sum`, `.env.example`, README, architecture, security, demo, seed notes | Pinned OIDC dependency and setup/security documentation. |

## Design decisions

The issuer URL is operator configuration. Discovery must return that exact issuer;
token-controlled `jku`, roles, tenant claims and usernames never select verification
keys or local identities. Keys come from the discovered JWKS URL. The HTTP client
has a five-second timeout and refuses redirects. URLs require HTTPS except literal
loopback hosts for development; HTTPS issuers cannot downgrade JWKS to HTTP.
The verifier pins RS256, verifies signature, issuer, audience, expiry and optional
`nbf` (the library permits two minutes of `nbf` clock skew), then requires a nonblank
subject, exact scope membership and payload `typ=Bearer`. This intentionally
supports Keycloak JWT access tokens and rejects its ID tokens. It is not a generic
opaque-token/introspection or every-provider integration.

`AUTH_AUDIENCE` must equal canonical `RESOURCE_URL`, whose path must be `/mcp`.
The fixture adds this audience explicitly; an arbitrary OAuth client's ID is not
the resource audience. Login sends RFC 8707's `resource` parameter; the demo realm
is preconfigured for this single audience rather than dynamically granting arbitrary
requested resources. Scope `knowledge:read` permits entry but grants no document ACLs.

The SDK serves public RFC 9728 metadata and emits `resource_metadata` on 401s.
Both `/.well-known/oauth-protected-resource/mcp` and the root fallback describe the
same canonical resource. Host/Forwarded headers never influence advertised URLs.
Clients discover Keycloak authorization/token endpoints from its OIDC metadata.
No local authorization/token endpoints are impersonated or proxied.

The pre-registered public client is `permission-mcp-cli`, requires S256 PKCE, and
accepts only `http://127.0.0.1:8765/callback`. Password/direct grants are disabled.
The login CLI prints a browser URL to stderr and the access token only to stdout;
it does not open a browser, store refresh tokens or cache credentials. Other MCP
hosts need a pre-registered client with their exact callback and PKCE settings;
unrestricted dynamic registration/client-ID metadata documents are not enabled.

Keycloak subjects are fixed UUIDs, not usernames. The SQL fixture creates four
separate local identities with the existing roles; it does not mutate or alias dev
users. Reapplying it inserts no duplicates but is not a general role reconciliation
tool: it can restore its four fixture grants. Never run it to preserve deliberate
revocations. The explicit subject mapper avoids Keycloak tokens without `sub` when
the default basic client scope is absent.

The provider caches signing keys and refreshes on unknown keys. Known cached keys
can validate tokens during an outage; unknown keys fail closed. Key removal is not
instant revocation of cached keys or issued tokens. The fixture access-token lifetime
is five minutes. Local deactivation and ACL changes still apply on every request.

## Setup prerequisites

Go 1.27.1+, PowerShell 7, Docker Compose v2 and a running Linux engine; free local
ports 5432, 8080, 8081 and 8765. Existing databases need migrations 001–004 (see
[seed notes](../seed/README.md)); Phase 5 itself adds no migration. The later Phase 4
revision adds migration 005, which current code also requires. Downloading the new
Go dependency and Keycloak image requires network access. Tests need localhost
sockets. Commands do not automatically load `.env`. Use a local development DB.

## Ordered validation commands and expected results

### 1. Start dependencies and provision OIDC users

```powershell
docker compose --profile oidc config --quiet
docker compose --profile oidc up -d
docker compose logs --tail 30 keycloak
Invoke-RestMethod http://127.0.0.1:8081/realms/permission-mcp/.well-known/openid-configuration
Get-Content -Raw seed/oidc-users.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
if ($LASTEXITCODE -ne 0) { throw 'OIDC fixture failed' }
go run ./cmd/ingest --tenant acme
```

Wait for the Keycloak startup log before discovery; `up -d` does not guarantee
Keycloak readiness. Expect issuer `http://127.0.0.1:8081/realms/permission-mcp`,
authorization/token/JWKS URLs, four local OIDC users and grants, and four ingested
documents. Reapplying the SQL inserts zero rows. A fresh volume imports the realm;
an existing realm is skipped, so editing the JSON does not update an existing realm.
Development admin: `admin` / `local-admin-only` at `http://127.0.0.1:8081/admin`.
Use the admin console to apply later configuration changes; preserve the DB volumes.

### 2. Configure and start the server

Run these exports in **both** the server and client terminal:

```powershell
$env:AUTH_MODE = 'oidc'
$env:AUTH_ISSUER = 'http://127.0.0.1:8081/realms/permission-mcp'
$env:RESOURCE_URL = 'http://127.0.0.1:8080/mcp'
$env:AUTH_AUDIENCE = $env:RESOURCE_URL
```

In the server terminal:

```powershell
go run ./cmd/server
```

Expect startup with `auth_mode=oidc`, without generating or loading local JWT keys.
An unreachable issuer or mismatched discovery issuer prevents startup.

### 3. Discover, log in and call MCP

```powershell
Invoke-RestMethod http://127.0.0.1:8080/.well-known/oauth-protected-resource/mcp
$challenge = Invoke-WebRequest http://127.0.0.1:8080/mcp -Method Post -SkipHttpErrorCheck
$challenge.StatusCode
$challenge.Headers['WWW-Authenticate']
$token = go run ./cmd/login
```

Expect resource metadata with the configured resource, issuer and scope; HTTP 401
with a canonical `resource_metadata` URL and scope. Open the login URL printed to
stderr. Sign in as `alice`, `bob`, `carol` or `dave`, password `local-demo-only`.
The callback displays a completion message and the command returns a five-minute
access token. For another caller, sign out in the browser or use a private window.
Use `$token` with the README's `whoami`, listing, document and search requests,
skipping its token-acquisition line if already signed in. Expect UUID subjects,
database roles and the original 2/2/4/1 source permission matrix. Alice's direct
salary read still returns `resource not found`; spoofed role arguments are rejected
and audited. `seed/prompt-injection-demo.ps1` accepts `-Token $token` in OIDC mode.

### 4. Run automated validation

```powershell
gofmt -l cmd internal
go vet ./...
$env:TEST_DATABASE_URL = 'postgres://permission_mcp:permission_mcp@localhost:5432/permission_mcp?sslmode=disable'
$env:TEST_OIDC_ISSUER = 'http://127.0.0.1:8081/realms/permission-mcp'
go test -count=1 -v ./...
go build ./...
git diff --check
```

Expect passing tests, clean formatting/vet/build/diff, and execution of
`TestKeycloakIntegration`, `TestAuditedInjectionIntegration`, and the SQL suites.
Without either test variable, the live Keycloak test skips. Without the DB variable,
SQL suites skip. Live tests use the local fixture password and exact realm/client
configuration. They never print access tokens or log login pages/authorization codes.
Existing retrieval tests re-ingest seeded content; avoid customized/production data.

## Automated test coverage

- `TestOIDCVerification`: real HTTP discovery/JWKS; required claims, algorithm and
  signature rejection, exact scope, missing subject/expiry, wrong audience/issuer,
  ID token rejection, future `nbf`, rotation, cached-key outage, unknown-key outage
  and canceled request. Extra tenant/role claims do not enter the identity object.
- `TestOIDCURLPolicy`, `TestOIDCConfiguration`: URL trust, discovery issuer mismatch,
  canonical audience/resource constraints, OIDC without dev keys, unsupported modes.
- `TestOAuthDiscovery`: root/path metadata, methods and unauthenticated access,
  canonical challenge and hostile Host/Forwarded header independence.
- `TestLoginPKCECallback`: HTTP provider fixture plus real loopback callback; bad
  state and response issuer rejected, correct S256 verifier/resource/client/redirect
  required during code exchange, stdout contains only the verified access token.
- `TestKeycloakIntegration`: actual Keycloak login form and PKCE exchange for four
  users across both MCP revisions; database identity, 2/2/4/1 listing, Alice salary
  denial, schema spoofing rejection. Uses an in-memory audit store; real persistent
  audits and injection/tenant checks remain covered by the Phase 4 SQL test.
- Existing auth, local resolver, ACL revocation, tenant isolation, retrieval and
  persistent audit suites continue unchanged. Manual browser interaction is not
  automated; the CLI callback uses an HTTP fixture while live Keycloak tests drive
  its actual login form and code exchange without a browser UI.

## Validation record — 2026-10-20

Downloaded the pinned Go dependency and Keycloak 26.8.1 image, validated Compose
configuration and started its optional profile alongside the existing healthy
PostgreSQL service. Keycloak imported the development realm. Created isolated DB
`phase5_validation_20261020`, applied migrations 001–004 and the OIDC user fixture.
The existing development database was not modified by this phase's validation.

The first live run correctly rejected Keycloak tokens missing `sub`; added the
explicit subject mapper and reimported only the new demo realm while Keycloak was
stopped. The expanded verbose suite then passed against real Keycloak/PostgreSQL,
including all eight user/protocol cases, source matrix, Alice salary denials,
spoofing, pgvector, tenant constraints and persistent audit/injection tests.
Sandboxed localhost tests failed with socket restrictions; authorized reruns passed.

Added the CLI callback test and corrected its mock metadata content type and
early-error output handling. The final uncached full suite passed with both test
variables set after state, response issuer and single-use callback protections.
`gofmt -l cmd internal`, `go vet ./...`, `go build ./...`, an explicit server build
to `.local/phase05-server.exe`, and `git diff --check` passed (Git reports only
configured LF-to-CRLF conversion notices).

Started that built server with OIDC on port 18085 against the temporary database,
with an intentionally nonexistent development key path. Readiness, canonical
resource metadata and the unauthenticated 401 discovery challenge passed. Stopped
the smoke server, confirmed eight users/eight role memberships/four documents in
the temporary DB, then removed that database. Keycloak and the original development
PostgreSQL service remain running with persisted volumes. Ignored executable and
sanitized startup logs remain under `.local/`.

The updated PowerShell injection script was syntax-checked; its new explicit-token
branch was not run end-to-end. No manual browser login, production deployment,
load/race test or third-party MCP host compatibility test was performed. Commands
above remain reproducible instructions; only this record claims executed checks.

## Troubleshooting

- Discovery unavailable: wait for Keycloak startup, inspect its logs, use exactly
  `127.0.0.1:8081` and the realm path. `localhost` is a different issuer string.
- Valid signature but 401: check subject, `typ=Bearer`, scope, expiry, resource
  audience and active local `(issuer, subject)` row. A username is not the subject.
  Missing `sub` means the realm lacks the subject mapper. Imported realms are not
  automatically updated from JSON on restart.
- Browser redirect rejected: use the pre-registered CLI or register your MCP host's
  exact callback; port 8765 must be free. Password grants are intentionally disabled.
- Wrong caller in browser: sign out or use a private window; Keycloak SSO may reuse
  the existing user's login. The CLI always verifies the resulting access token.
- Login timeout: retry and finish within three minutes; do not print callback URLs
  or tokens in issue reports. Tokens expire after five minutes; log in again.
- Existing audit/readiness failures: migration 004 and audit INSERT permissions
  are still required. Readiness checks DB connectivity, not OIDC or schema health.
- Restricted test sockets/Docker access: rerun with the required execution approval;
  skipped SQL/OIDC tests do not establish live integration correctness.

## Limitations and deferred work

Local `start-dev`, H2 storage, public demo passwords and bootstrap admin credentials
are development fixtures. APP_ENV still rejects production. Remote deployment needs
TLS, hardened identity-provider/database operations, secrets, ingress policy and
rate limits. No opaque tokens/introspection, generic non-Keycloak access-token
profiles, unrestricted client registration, automatic browser launch, refresh-token
storage, session logout propagation, immediate JWT revocation, authentication-failure
audit, key-refresh rate limiting or production deployment is included. Unknown-key
traffic can trigger JWKS refreshes; cached keys may outlive issuer key removal.
The login CLI has a fixed callback/client and no refresh/session persistence.
The SQL fixture assumes the exact local issuer/subjects; other providers need
explicit operator provisioning. Phase 6 defenses and external embedding providers
remain deferred.

References: [MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization),
[RFC 9728](https://www.rfc-editor.org/rfc/rfc9728.html),
[Keycloak containers](https://www.keycloak.org/server/containers),
[Keycloak subject mapper](https://www.keycloak.org/docs/26.8.1/upgrading/).
