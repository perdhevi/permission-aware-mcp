# Development identities

The initial migration seeds tenant `acme` and active subjects `alice`, `bob`,
`carol` and `dave`, all under issuer `http://dev.local`. User and tenant IDs are
generated UUIDs. Roles and documents belong to later phases.

`docker compose up -d --wait` mounts migrations into PostgreSQL's initialization
directory. Scripts execute only when the persistent data volume is empty. An
existing volume retains user state; restarting Compose never resets active flags.

For an existing **empty schema**, apply the initial migration explicitly:

```powershell
Get-Content -Raw migrations/001_initial.sql | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U permission_mcp -d permission_mcp
```

The migration is transactional and intentionally not repeatable: existing tables
cause failure rather than silently resetting identities. Do not apply it again to
an initialized schema. No separate seed command is required for this milestone.
