# Architecture

Implemented:

```text
MCP Client
   |
   | bearer token on every HTTP request
   v
Auth Middleware (official SDK RequireBearerToken)
   |
   | RS256 validation -> Identity
   v
Identity Resolver
   |
   v
User Repository (issuer + subject lookup)
   |
   v
Principal (database user ID, tenant, roles, token scopes)
   |
   v
MCP Tools (whoami, list_accessible_sources, get_document, search_knowledge)
```

`internal/authn` validates tokens and resolves an active local identity. The
`UserRepository` abstraction keeps business logic independent of pgx. The PostgreSQL
adapter joins tenants using the user's database foreign key and uses parameterized SQL.
Unknown or inactive identities fail closed; database failures return a generic 500.
These are identity eligibility checks, not document authorization policy.

`internal/mcpserver` adapts verification to official SDK bearer middleware and
Streamable HTTP. `TokenInfo.UserID` is the database user UUID, not a client value.
The principal is stored in `TokenInfo.Extra`; tool handlers read `req.Extra.TokenInfo`.
The SDK provides schemas, JSON-RPC and protocol negotiation.

The latest stable SDK selected for this pass is v1.8.0. Protocol `2026-07-28`
requires stateless Streamable HTTP; legacy `2025-11-25` also works. Each request has
a fresh verified identity and database lookup, with no session identity caching.
Token expiry, deactivation and tenant membership are therefore checked again on
subsequent requests. Per-request logs correlate HTTP and tool calls via generated
request IDs. No headers, tokens or request bodies are logged.

Document read path (implemented in Phase 2):

```text
MCP Tool
   |
   v
Authorization (tenant + database roles + document ACL)
   |
   v
Document Repository (same tenant + active user + explicit user/role read grant)
   |
   v
PostgreSQL + pgvector
```

`internal/authz.Service` validates the principal and resource arguments and invokes
permission-constrained reads. The repository rechecks active user membership and
current role/ACL records in the same SQL statement that returns data. There is no
separate permission-check/read race and no policy cache. Principal roles are for
`whoami`; read decisions use the current database state. `EXISTS` avoids duplicate
sources when several grants match. The outer tenant predicate applies to both
listing and direct reads, before ordering and limiting.

Composite foreign keys prevent cross-tenant role assignments and ACL grants.
ACLs use exactly one nullable `user_id` or `role_id` with real foreign keys rather
than an unchecked polymorphic UUID. No role has implicit privileges.
`internal/rag.Embedder` has a deterministic offline `dev-hash-v1` implementation
with 128 normalized word-hash buckets. `rag.Retriever` validates the principal,
query and limit, embeds the query, validates the vector, then calls the repository.
Search shares the exact document read predicate. A materialized authorized CTE
precedes cosine ranking and LIMIT, with no approximate index or post-filtering.
Model and dimensions must match. Content hashes exclude stale chunks after edits.

The trusted `cmd/ingest --tenant acme` CLI reads existing database documents,
splits them into 1000-character windows with 100-character overlap, and replaces
chunks under a per-document transaction and row lock. IDs and ACLs are preserved.
It is repeatable without duplicate chunks; chunk UUIDs change on replacement.
Failure rolls back that document; earlier committed documents remain ingested.
The server never performs generation or ingests client-supplied documents.

Compose applies 001, 002 and 003 on a fresh volume. Existing databases must
apply missing migrations explicitly following [seed notes](../seed/README.md). Migrations are
transactional, ordered SQL files; there is no automatic version runner. Startup
checks connectivity, so schema validation remains an operator prerequisite.

SDK references: [releases](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)
and [protocol guide](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md).
