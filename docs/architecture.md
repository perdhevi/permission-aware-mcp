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
Principal (database user ID, tenant, token scopes)
   |
   v
MCP Tool (whoami)
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

Future components (not implemented):

```text
MCP Tool
   |
   v
Authorization (tenant + database roles + document ACL)
   |
   v
Permission-aware RAG (authorized candidates before ranking)
   |
   v
PostgreSQL + pgvector
```

`internal/authz` is only a documented package boundary. `internal/rag.Embedder`
defines the future embedding contract without selecting or implementing a provider.
No ingestion, semantic retrieval or resource authorization is exposed yet.

The Compose database initializes the initial migration on a fresh volume. A future
schema-change phase should introduce versioned migration execution before adding
further migrations; this milestone does not silently migrate existing volumes.

SDK references: [releases](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)
and [protocol guide](https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md).
