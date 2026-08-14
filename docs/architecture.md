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

Each feature (auth, user, organization, membership, client, project, milestone,
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
    │                sets user_id / session_id / must_change_password
    │                in request context
    ▼
RequirePasswordChanged  ──►  403 password_change_required for users with
    │                         must_change_password = true (every protected
    │                         route except POST /v1/me/password)
    ▼
OrgMiddleware.LoadOrg  ──►  parses {orgID}, verifies the org exists and
    │                        is active, sets organization_id in context
    ▼
OrgAccess.RequireMembership  ──►  verifies an active membership exists
    │                              for the user in this org
    ▼
RBAC.RequirePermission(key)  ──►  checks the membership's role holds the
    │                              permission key, records the decision in
    │                              authz_decisions, attaches the role name
    │                              to the request context
    ▼
Handler
```

The cookie-authenticated routes (`POST /v1/auth/refresh`, `logout`,
`logout-all`) are registered without `RequireAuth`, so they are not gated and
remain usable by a user who still must rotate their one-time credential.
`POST /v1/me/password` is the single protected route exempt from the gate.

## Multi-Tenancy

Tenancy is organization-scoped. Almost every business table carries an
`organization_id` column, and tenant isolation is enforced at two layers:

1. **Middleware** — `LoadOrg` resolves the org from the URL path and rejects
   requests for missing or non-active organizations before any business logic
   runs.
2. **Query level** — services/repositories always filter by the resolved
   `organization_id` from the request context, so a member can never read or
   mutate another tenant's rows even if they guess a foreign UUID.

Within a tenant, **client-role actors are additionally project-scoped**:
services compare `projects.client_id` against the actor's user ID and resolve
any mismatch to `404 project_not_found` — the 404-no-leak convention means a
client cannot distinguish another client's project from a nonexistent one.
Scope denials are recorded as denied `authz_decisions` rows carrying the
resource identity (see [Auditability](#auditability)). The permission system
has no resource dimension, so this enforcement lives in the service layer
(`ensureActorProjectAccess` / `ensureActorProjectAccessInTx`), keyed on the
role name the RBAC middleware attaches to the request context.

## RBAC Model

Roles and permissions are data-driven (no hard-coded enums in code):

- `permissions` — atomic actions, e.g. `project.update`, `member.remove`,
  `milestone.submit`, `client.provision`, `activity.project.list`.
- `roles` — named sets; `organization_id IS NULL` rows are system template
  roles (`owner`, `admin`, `member`, `viewer`, plus `client`). Org-scoped
  custom roles can be layered on later.
- `role_permissions` — join table granting permissions to roles.
- `memberships` — links a user to an org and points at the role that governs
  their access.

The `RBAC.RequirePermission(key)` middleware performs a single SQL `EXISTS`
check and writes every decision (allowed or denied, with a reason) to
`authz_decisions` for auditability.

The `client` role is a narrow, project-scoped surface: it holds only
`project.view`, `milestone.view`, `milestone.approve`,
`milestone.revision.request` and `activity.project.list`, and is never granted
list, org, member, or assignment keys. Because the permission system has no
resource dimension, client *project scope* is enforced in the service layer
(see [Multi-Tenancy](#multi-tenancy)), and `member.list` excludes client
memberships at the query level. `milestone.approve` /
`milestone.revision.request` are additionally restricted at the service layer
to the project's assigned client regardless of what RBAC grants.

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
  `membership.removed`, `milestone.approved`, ...) with actor, entity, and JSON
  metadata. Written inside the same transaction as the mutation.
- `authz_decisions` — every authorization decision, allowed or denied, with the
  permission key and reason.

An `activity` feed is denormalized for the product UI, keyed by organization and
project, containing a human-readable message and JSON metadata. The historical
metadata-drop defect (`activity.NewActivity` hardcoding an empty object) is
fixed — metadata passed by callers is now persisted.

### Versioned audit events (Layer-1 convention)

Layer-1 mutations (client lifecycle, approval state machine, deliverables,
payment status) write a standardized metadata object via
`audit.VersionedMetadata`:

```json
{
  "schema_version": 1,
  "before": { "...": "pre-mutation state" },
  "after":  { "...": "post-mutation state" },
  "reason": "optional explanation"
}
```

`before`/`after` hold the relevant pre/post state of the mutation (e.g. the
milestone's old and new status); `reason` is omitted when empty. Stable action
keys are used throughout, e.g. `milestone.submitted`, `milestone.approved`,
`milestone.changes_requested`, `milestone.status_changed`,
`deliverable.submitted`, `deliverable.removed`,
`milestone.payment_status_changed`, `project.client_assigned`,
`project.client_removed`, `client.provisioned`, `client.credential_rotated`.
The milestone state machine writes an audit row on every transition — that row
is the status-transition history. An index on
`audit_logs(entity_type, entity_id, created_at DESC)` (migration 000005) backs
per-entity history queries.

### Scope denials with resource identity

The RBAC middleware records decisions with an empty resource identity. When the
service layer denies a request that RBAC cannot express — a client actor
outside their project scope — it records a denied `authz_decisions` row via
`audit.RecordDecision` with `resource_type`/`resource_id` populated (e.g.
`resource_type = "project"`, `resource_id = <projectID>`), so denial analytics
can answer per-resource questions.

## Milestone Approval State Machine

Milestones carry an approval lifecycle on top of the plain work statuses.
There are three action endpoints and one generic staff endpoint; the
`milestone_status` enum is `pending`, `in_progress`, `awaiting_approval`,
`completed`, `blocked`, `cancelled`, `approved`, `changes_requested`.

- `POST .../milestones/{id}/submit` — staff (`milestone.submit`). Valid from
  `in_progress` or `changes_requested`. Requires the project to have an
  assigned client (`400 project_has_no_client`) and the milestone to carry at
  least one deliverable (`400 deliverable_required`). Creates a
  `milestone_revisions` row, increments `revision_count`, moves the milestone
  to `awaiting_approval`. Idempotent: an already-`awaiting_approval` milestone
  is a no-op (no duplicate revision row, no counter increment).
- `POST .../milestones/{id}/approve` — the client's sign-off
  (`milestone.approve`). Valid only from `awaiting_approval`; moves to
  `approved`. Idempotent when already `approved`. The service additionally
  requires the actor to be the project's assigned client, regardless of RBAC
  grants.
- `POST .../milestones/{id}/changes-requested` — the client asks for revisions
  (`milestone.revision.request`). Valid only from `awaiting_approval`; moves
  to `changes_requested`. **Not** idempotent: a stale request returns
  `409 milestone_not_awaiting_approval`.
- `PATCH .../milestones/{id}/status` — generic staff transition
  (`milestone.status.update`, owner/admin). The action-only statuses
  (`awaiting_approval`, `approved`, `changes_requested`) are rejected as
  targets; `approved → completed` stamps `completed_at`; `completed` and
  `cancelled` are terminal. Same-state requests are no-ops.

All four paths block on a project in `archived` or `cancelled` state
(`400 invalid_status_transition`). Every transition is guarded by the current
status in the UPDATE (`WHERE status = $current`), so concurrent stale writers
get zero rows; the milestone row is locked `FOR UPDATE` for the duration of
the transaction.

Generic staff transitions (`PATCH .../status`):

| Current | Allowed targets |
|---------|-----------------|
| `pending` | `in_progress`, `cancelled` |
| `in_progress` | `pending`, `blocked`, `cancelled` |
| `blocked` | `in_progress`, `cancelled` |
| `awaiting_approval` | `blocked`, `cancelled` (escape hatch; never `completed`) |
| `changes_requested` | `in_progress`, `blocked`, `cancelled` |
| `approved` | `completed` |
| `completed` / `cancelled` | — (terminal) |

### Revision semantics

- `revision_count` = number of **submission rounds** (initial submission = 1,
  each resubmission = +1). One `milestone_revisions` row is created per
  submission with `submitted_by`/`submitted_at` = the agency actor/time; the
  table is append-only and is the revision-history artifact.
- `revision_limit` is the maximum submission rounds: a per-milestone override,
  else the project default, else `NULL` (unlimited). An explicit `0` is
  forbidden by CHECK constraint (`revision_limit IS NULL OR revision_limit >= 1`).
- `limit_reached` is **computed at read** (`revision_count >= effective limit`,
  never stored) and only surfaced to staff actors.

### Status values duplicated in enum + CHECK

`milestone_status` values exist in two places — the enum type and the
`milestones_status_check` CHECK constraint (the same applies to
`milestone_payment_status` and its CHECK). Migration 000005 extended the enum
via a full column rewrite (enum → TEXT → new enum) in both directions, because
`ALTER TYPE ... ADD VALUE` is not reversible and its new values cannot be used
inside the applying transaction. Any future status change must keep the enum
and the CHECK in sync and plan a column rewrite.

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
  client/                client provisioning + credential lifecycle (handler, service, repo, dto, model)
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
