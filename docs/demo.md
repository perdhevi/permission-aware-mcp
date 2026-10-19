# Phase 1 demo

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

Automated tests cover these boundaries and both current and legacy protocol clients.
The command in README enables real database integration tests. No document retrieval,
role assignments or prompt-injection retrieval demo exists until later phases.
