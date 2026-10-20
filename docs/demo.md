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
revocation and cross-tenant tests. Semantic search and the complete prompt-injection
retrieval demo follow in later phases.
