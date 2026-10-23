# Authentication and authorization demo

Follow the exact setup and PowerShell call in [README](../README.md).

1. Start PostgreSQL, generate keys once, mint an Alice token, then start the server.
2. Check `/healthz` and `/readyz` return 200 without credentials.
3. Call `/mcp` without Authorization: expect 401.
4. Set `Authorization: Bearer invalid`: expect 401.
5. Call `whoami` with Alice's valid token and `{}`: expect Alice's local UUID,
   tenant `acme`, scope `knowledge:read`.
6. Mint with `--subject bob`, replace the header, and repeat: expect Bob's UUID.
7. Keep Alice's token and add `user_id`, `tenant_id` or `role` to `arguments`:
   expect schema rejection. Calling again with `{}` still returns Alice.
8. Mint with an unknown subject: signature validation succeeds but local user
   resolution rejects the request with 401.

Use `go run ./cmd/devtoken mint --subject alice --ttl 1s`, wait until it expires,
and call again: expect 401. Never put real bearer tokens in demo documents or logs.

Continue with the authorized read examples in [README](../README.md) and the
complete [Phase 02 guide](phase-02.md). Repeat listing with each caller's token:
Alice sees Company Handbook and Engineering Roadmap; Bob sees Company Handbook
and Salary Guidelines; Carol sees all four; Dave sees Company Handbook.

As Carol, save the UUID of Salary Guidelines, then switch to Alice's token and
call `get_document` with that UUID. Expect `isError: true` and `resource not found`.
Bob receives the same error for Engineering Roadmap. Knowing a UUID or having
previously listed a source does not grant access. Extra identity/role/tenant
arguments are rejected on both tools.

Automated tests cover these boundaries and both current and legacy protocol
clients. Enable `TEST_DATABASE_URL` to run the real database permission matrix,
revocation and cross-tenant tests.

For Phase 3, apply migration 003 and run `go run ./cmd/ingest --tenant acme`.
Follow the README search example with query `salary` and limit 20. Switch tokens
between Alice, Bob, Carol and Dave: expect 2, 2, 4 and 1 authorized chunks. Alice
receives no Salary Guidelines title or content, even though it is an exact lexical
match. With query `deployment automation`, Alice can retrieve Engineering Roadmap;
Bob cannot. The offline hash embedder has no relevance threshold, so other allowed
chunks can appear. It is a policy demo rather than a language-understanding model.
See [Phase 03](phase-03.md).

## Audited prompt-injection demo (Phase 4)

Apply migrations 004 and 005 once following [seed notes](../seed/README.md). The following
optional fixture appends hostile instructions to the existing Engineering Roadmap,
using the actual Salary Guidelines UUID. It preserves the four-document permission
matrix, existing content and grants; rerunning the fixture does not append twice.
Use a local demo database. Content edits suppress old chunks until re-ingested.

```powershell
Get-Content -Raw seed/prompt-injection.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
if ($LASTEXITCODE -ne 0) { throw 'Demo fixture failed' }
go run ./cmd/ingest --tenant acme
if ($LASTEXITCODE -ne 0) { throw 'Ingestion failed' }
go run ./cmd/server
```

In a second PowerShell 7 terminal at the repository root:

```powershell
./seed/prompt-injection-demo.ps1
```

The script mints an Alice token, retrieves the hostile text, extracts its target
UUID, and simulates a host following that instruction by calling `get_document`.
Expect `resource not found`, then schema rejection when it tries to claim Carol's
identity/executive role. A final `whoami` still returns Alice. It prints one PASS
line and never prints the token. Use `-Endpoint http://127.0.0.1:18084/mcp` for
an alternate server address. This tests the backend boundary even when a host
follows hostile instructions; it does not measure a model's resistance to injection.

Inspect the persisted decisions as a trusted operator:

```powershell
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT a.timestamp,a.request_id,u.subject,a.tool,a.resource_id,a.decision,a.reason,a.query_hash,a.disclosed_document_ids,a.duration_ms FROM audit_log a LEFT JOIN users u ON u.id=a.user_id WHERE u.subject='alice' ORDER BY a.timestamp DESC,a.id DESC LIMIT 10;"
```

Expect ALLOW for identity/search, DENY `not_found_or_forbidden` for the injected
read, and DENY `invalid_arguments` for spoofing. Query hashes are 64 hex characters;
raw queries and document bodies have no audit columns. The search row's
`disclosed_document_ids` lists Engineering Roadmap (and any other returned sources)
but never Salary Guidelines; denied and spoofed rows show NULL. Denial does not reveal
whether the target exists. An empty authorized search/list is an ALLOW decision.
Audit writes add up to two seconds to the bounded read; a write failure withholds
results and reports `resource service unavailable`.

To replicate disclosures for all four seeded callers and every tool, run
`./seed/audit-disclosure-demo.ps1` against the same `AUTH_MODE=dev` server; it
compares each persisted row with the permission matrix and prints one PASS line.
See [Phase 04 test and replication steps](phase-04.md#test-and-replication-steps).

For automated, isolated tenant fixtures without modifying the demo corpus, run
`TestAuditedInjectionIntegration` as described in [Phase 04](phase-04.md). To remove
the optional text, restore Engineering Roadmap content from a saved copy and
re-ingest. The fixture does not retain a backup or automatically remove content.

## Keycloak login (Phase 5)

This optional mode uses Keycloak rather than locally minted tokens; the default
demo above stays offline in `AUTH_MODE=dev`. Follow
[Phase 05](phase-05.md) to start the optional Compose profile, provision OIDC users,
export the OIDC settings in both terminals, and run `go run ./cmd/login`. Complete
the browser login as Alice with `local-demo-only`. Use the returned access token
with the README's MCP requests. Her subject is a UUID; roles and ACLs still come
from PostgreSQL. For the optional hostile fixture above, run:

```powershell
./seed/prompt-injection-demo.ps1 -Token $token
```

The script accepts Alice's dev or stable Keycloak subject, checks the caller stays
unchanged, and sends the supplied bearer token on every request. In OIDC mode it
requires an explicit token. This remains a deterministic host simulation.
