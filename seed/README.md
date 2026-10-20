# Development identities and documents

The initial migration seeds tenant `acme` and active subjects `alice`, `bob`,
`carol` and `dave`, all under issuer `http://dev.local`. User and tenant IDs are
generated UUIDs. Migration 002 assigns Alice `engineering`, Bob `hr`, Carol
`executive`, and Dave `employee`, and creates four documents and explicit read ACLs.
See the matrix and tool examples in [Phase 02](../docs/phase-02.md).

`docker compose up -d --wait` mounts migrations into PostgreSQL's initialization
directory. Scripts execute only when the persistent data volume is empty. An
existing volume retains user state; restarting Compose never resets active flags.

For an existing **empty schema**, apply the initial migration explicitly:

```powershell
Get-Content -Raw migrations/001_initial.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
```

Each migration is transactional and intentionally not repeatable: existing tables
cause failure rather than silently resetting identities. Do not apply it again to
an initialized schema. There is no automatic upgrade runner or separate seed command.

## Upgrade an existing phase 1 database

Start Compose, then inspect the target schema:

```powershell
docker compose up -d --wait
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp -c "SELECT to_regclass('public.users') AS users, to_regclass('public.roles') AS roles;"
```

If `users` exists and `roles` is null, apply only the new migration:

```powershell
Get-Content -Raw migrations/002_authorization.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
if ($LASTEXITCODE -ne 0) { throw 'Migration 002 failed; inspect the error before retrying' }
```

Expect `COMMIT`. If `roles` already exists, do not reapply migration 002. Fresh
volumes automatically run 001 then 002. For an existing empty schema, apply 001
using the command above, then 002. Never remove a volume to upgrade it. The seed
targets the development issuer `http://dev.local`; custom issuer users require
explicit role assignments. Role names alone grant no access without an ACL.
