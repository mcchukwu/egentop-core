# Architecture

Egentop-Core is a multi-tenant project management API built with Go and
PostgreSQL. It follows a clean, layered architecture with strong multi-tenancy
and auditability.

## Layered Overview

```text
HTTP client
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ HTTP / transport layer                                      │
│  - middleware chain (recovery, request ID, logging,         │
│    security headers, CORS, rate limiting)                   │
│  - handlers decode/validate requests and write responses    │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Service layer (business rules, transactions)                │
│  - orchestrates repositories, validates invariants,         │
│    enforces tenant scope, writes audit + activity events    │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ Repository layer (SQL)                                      │
│  - data access, row <-> model mapping                       │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│ PostgreSQL                                                  │
│  - multi-tenant schema with organization_id scoping         │
│  - triggers maintain updated_at and audit/activity tables   │
└─────────────────────────────────────────────────────────────┘
```

Each feature (auth, user, organization, membership, project, milestone,
assignment, activity) is a self-contained package under `internal/` exposing a
handler, a service, a repository and DTO/model types.

## Middleware Chain

Requests pass through a chain assembled in `cmd/api/main.go`:

1. `Recovery` — recovers panics and returns 500.
2. `RequestID` — assigns a request ID header.
3. `Logging` — structured request logging.
4. `SecurityHeaders` — sets security response headers.
5. `CORS` — validates origins against `CORS_ALLOWED_ORIGINS`.
6. `RateLimiter` — in-memory per-IP limiting (100/min default).
7. Route-specific middleware.

### Authentication & Authorization Flow

```text
Access token (JWT)
    │  Authorization: Bearer <token>
    ▼
AuthMiddleware  ──►  validates JWT, loads user + session,
    │                sets user_id / session_id in request context
    ▼
OrgMiddleware.LoadOrg  ──►  parses {orgID}, verifies the org exists and
    │                        is active, sets organization_id in context
    ▼
OrgAccess.RequireMembership  ──►  verifies an active membership exists
    │                              for the user in this org
    ▼
RBAC.RequirePermission(key)  ──►  checks the membership's role holds the
    │                              permission key, records the decision in
    │                              authz_decisions
    ▼
Handler
```

## Multi-Tenancy

Tenancy is organization-scoped. Almost every business table carries an
`organization_id` column, and tenant isolation is enforced at two layers:

1. **Middleware** — `LoadOrg` resolves the org from the URL path and rejects
   requests for missing or non-active organizations before any business logic
   runs.
2. **Query level** — services/repositories always filter by the resolved
   `organization_id` from the request context, so a member can never read or
   mutate another tenant's rows even if they guess a foreign UUID.

## RBAC Model

Roles and permissions are data-driven (no hard-coded enums in code):

- `permissions` — atomic actions, e.g. `project.update`, `member.remove`.
- `roles` — named sets; `organization_id IS NULL` rows are system template
  roles (`owner`, `admin`, `member`, `viewer`). Org-scoped custom roles can be
  layered on later.
- `role_permissions` — join table granting permissions to roles.
- `memberships` — links a user to an org and points at the role that governs
  their access.

The `RBAC.RequirePermission(key)` middleware performs a single SQL `EXISTS`
check and writes every decision (allowed or denied, with a reason) to
`authz_decisions` for auditability.

## Sessions & Token Rotation

- **Access token** — short-lived JWT (default 15m) in the `Authorization`
  header.
- **Refresh token** — stored as a rotating, HttpOnly, SameSite=Lax cookie.
- **Sessions table** — each session tracks a `token_family_id`, a hashed
  refresh token, device metadata and revocation state. Rotating a token issues
  a new hash within the same family. Reusing a revoked token revokes the entire
  family (a theft signal).

## Auditability

Two complementary tables:

- `audit_logs` — business events that happened (`organization.updated`,
  `membership.removed`, ...) with actor, entity, and JSON metadata. Written
  inside the same transaction as the mutation.
- `authz_decisions` — every authorization decision, allowed or denied, with the
  permission key and reason.

An `activity` feed is denormalized for the product UI, keyed by organization and
project, containing a human-readable message and JSON metadata.

## Transactions

Mutations that touch multiple tables (e.g. create project + audit log +
activity) run inside a single `*sql.Tx` via `db.WithTransaction`. Postgres
quirks are handled explicitly: a unique-violation inside a transaction aborts
the whole transaction, so slug-retry loops use savepoints to retry inside the
same transaction.

## Package Layout

```text
cmd/api/                 HTTP server bootstrap, middleware chain, route table
internal/
  activity/              activity feed (handler, service, repo)
  apperrors/             sentinel errors shared across layers
  assignment/            assignments (handler, service, repo, dto, model)
  audit/                 audit log writer
  auth/                  register/login/refresh/logout (handler, service, dto)
  health/                liveness/readiness probes
  jwt/                   JWT manager and claims
  membership/            org membership + role resolution (handler, service, repo, dto, model)
  middleware/            auth, org load, org access, RBAC, CORS, rate limit, ...
  normalize/             phone/email normalization
  organization/          organizations (handler, service, repo, dto, model)
  project/               projects + milestones (handler, service, repo, dto, model)
  requestctx/            typed values carried in the request context
  response/              JSON envelopes, error mapping, pagination
  slug/                  slug generation
  user/                  user profile (handler, service, repo, dto, model)
  validation/            validator wiring and custom rules
pkg/
  config/                env-driven configuration + validation
  db/                    connection, transaction helper, PG error helpers
  logger/                logging setup
  pagination/            page/limit parsing and metadata
migrations/              ordered SQL migrations
```

## Cross-Cutting Concerns

- **Errors** — packages return sentinel errors from `internal/apperrors`;
  `internal/response` maps them to HTTP status codes and JSON error bodies.
- **Validation** — DTOs carry `validate` tags; `validation.ValidateStruct`
  produces field-level messages.
- **Timing** — every query runs under a context timeout (`db.WithDBTimeout`,
  5s default).
- **Time handling** — `time.Time` values are stored as `TIMESTAMPTZ`; requests
  accept RFC 3339 strings and responses emit RFC 3339.
