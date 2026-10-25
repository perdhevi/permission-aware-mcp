# Phase 06 — Topic isolation PoC (same query, different documents)

## Scope and outcome

Demonstrate that when one user is granted a document, other users are not, even
when every caller asks about the same topic. An optional fixture adds one topic,
"Project Falcon", with a shared overview and three confidential documents that are
each readable by exactly one named user. A replication script runs the same search
for all four seeded callers and cross-checks direct reads of every Falcon document.

No server code, schema or tool changes. The PoC uses the existing user-level grant
path (`document_acl.user_id`), which the `readable` SQL filter in
`internal/repository/postgres/documents.go` already applies to listing, direct reads
and search.

## Changes by file/component

| Files | Changes |
| --- | --- |
| `seed/topic-isolation.sql` | Optional, rerunnable fixture: four `falcon-*` documents in tenant `acme`; overview granted to all four roles; Technical Design → `alice`, Staffing Plan → `bob`, Budget Approval → `carol`, each as a user grant with no role grant. |
| `seed/topic-isolation-cleanup.sql` | Deletes the four `falcon-*` documents; ACL rows and chunks cascade. |
| `seed/topic-isolation-demo.ps1` | Mints dev tokens for the four callers, runs one search per caller, prints the Falcon titles each receives, prints a caller × document direct-read matrix, and fails unless results match the expected matrix. |
| `README.md` | Phase guide link. |

## Design decisions

- **User grants, not role grants.** Each confidential Falcon document is granted to
  one user ID. Two people with the same role would still differ, and Carol's
  `executive` role does not see Alice's or Bob's Falcon documents. This shows there
  is no role hierarchy or global bypass: access is exactly what the ACL lists.
- **One topic, shared vocabulary.** Every Falcon document's *content* contains
  "Project Falcon" and "migration", so all four are good matches for the same query.
  Titles are not embedded. Differences in results come only from authorization, not
  from relevance.
- **Shared overview.** Every caller receives at least one Falcon result. So "no
  result" isn't the only signal, and the comparison shows *which* documents differ.
- **Separate fixture.** The original four-document matrix and integration tests stay
  unchanged. The fixture is additive and removed by its cleanup script.

## Setup prerequisites

The local stack from the README: Docker Compose PostgreSQL with migrations 001–005,
`go run ./cmd/server` with `AUTH_MODE=dev`, and the four seeded dev users. Windows
PowerShell 5.1 or PowerShell 7. No network access is needed.

## Ordered validation commands and expected results

```powershell
Get-Content -Raw seed/topic-isolation.sql | docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
go run ./cmd/ingest --tenant acme
./seed/topic-isolation-demo.ps1
```

Expect `Ingested 8 documents using dev-hash-v1 (128 dimensions)`, then:

```text
Search: 'Project Falcon migration'
  alice  Project Falcon Overview, Project Falcon Technical Design
  bob    Project Falcon Overview, Project Falcon Staffing Plan
  carol  Project Falcon Budget Approval, Project Falcon Overview
  dave   Project Falcon Overview

Direct reads (get_document):
  Document                         alice   bob     carol   dave
  Project Falcon Budget Approval   denied  denied  READ    denied
  Project Falcon Overview          READ    READ    READ    READ
  Project Falcon Staffing Plan     denied  READ    denied  denied
  Project Falcon Technical Design  READ    denied  denied  denied

PASS: same query, four different result sets; each confidential Falcon document is readable by exactly one caller.
```

"denied" is the generic MCP tool error `resource not found`, the same answer as for
a nonexistent ID. The script exits with an error (`FAIL: …`) if any search result or
read differs from the expected matrix. Use `-Query '<text>'` to try other wording.
Every call is audited as in Phase 04.

To remove the fixture:

```powershell
Get-Content -Raw seed/topic-isolation-cleanup.sql | docker compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
```

Expect no output; `SELECT count(*) FROM documents WHERE source LIKE 'falcon-%'`
returns 0. Re-ingesting is not needed after cleanup, because chunks cascade.

## What was actually validated (2026-10-25, Windows PowerShell 5.1, local dev stack)

- Fixture applied, `Ingested 8 documents`, demo printed exactly the output above and `PASS`.
- Reapplying the fixture inserted nothing: 7 Falcon ACL rows (4 role, 3 user) before and after.
- `go test -count=1 ./...` with `TEST_DATABASE_URL` **while the fixture was applied**:
  `TestDocumentsIntegration` and `TestSearchIntegration` failed, because they assert
  the exact seeded matrix (for example, Alice listed the two Falcon documents too).
  This is expected.
- After cleanup: 0 Falcon documents, 4 chunks remaining, and
  `go test -count=1 ./internal/repository/...` passed.
- The database was left **without** the fixture.

Not exercised: OIDC mode, PowerShell 7.

## Automated test coverage

No new Go tests. The demo script is the replication check. The user-grant branch of
the `readable` filter is the same SQL already covered by the repository integration
tests. Those tests don't seed a user-level grant, so this script is currently the
only end-to-end check of that branch.

## Troubleshooting

- **`Expected 4 Falcon documents … found 0`:** the fixture is missing or acme was
  not re-ingested after applying it.
- **A caller sees fewer Falcon results:** the search `limit` is 10 and each caller
  has at most 6 readable documents, so this points to missing chunks. Re-ingest.
- **HTTP 401:** the server is not running in `AUTH_MODE=dev`, or a seeded user was
  deactivated (check `SELECT subject, active FROM users;`).
- **Repository integration tests fail with Falcon titles:** run the cleanup script.

## Limitations and deferred work

- User grants target the `http://dev.local` identities only. Keycloak identities from
  `seed/oidc-users.sql` receive the overview through their roles, not the exclusive
  documents.
- No Go integration test for user-level grants yet.
- Cleanup does not remove audit rows. They keep the historical disclosed document IDs.
- Demo data only. Do not run against production content.
