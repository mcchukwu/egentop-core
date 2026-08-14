# Egentop — Architecture

> Technical architecture. Confirmed = verified in code. Proposed = discussed, not built. Unknown = not established.
> Last updated: 2026-08-14 (Layer-1 delta built)

## Architectural Principles (Confirmed)

- Clean layering: handler → service → repository; each feature is a self-contained package under `internal/`
- Multi-tenant isolation enforced at the **query level** (services/repositories always filter by `organization_id` from request context); middleware is a second layer, never the only one
- **Client-role actors are additionally project-scoped at the service layer** (the permission system has no resource dimension): `projects.client_id == actor` or 404 not-found, never 403 — no existence leak; denials recorded in `authz_decisions` with resource identity
- Nested-resource boundaries are validated in the service layer: parent projects must belong to the active organization before nested lists are returned
- Transactional updates validate and lock the organization-scoped row (`FOR UPDATE`) before applying changes, keeping update validation and audit context consistent; state-machine writes are additionally status-guarded (`UPDATE ... WHERE status = <expected>` + RowsAffected check)
- Audit everything: every authz decision recorded, every business mutation audited in-transaction; Layer-1 events use versioned metadata (`{"schema_version":1,"before","after","reason"}`)
- Defense in depth (auth uses a lookup hash + authoritative bcrypt check; HMAC-only JWT signing)
- Config surface must be truthful — no flags that don't work
- No premature infrastructure: single instance, in-memory rate limiting, no Redis/K8s yet

## Major Components (Confirmed)

| Component | Responsibility |
|---|---|
| `cmd/api` | Bootstrap, config load, DB connect, middleware chain assembly; route registration in `cmd/api/routes.go` (data-driven `protectedRoutes` table with a `gated` flag — test-enforced invariant) |
| `internal/auth` | Register, login, refresh (rotation), logout, logout-all |
| `internal/user` | Profile, update, password change (revokes other sessions; clears `must_change_password`) |
| `internal/organization` | Org CRUD; default org + owner membership created at registration |
| `internal/membership` | Add/invite/list/role-update/remove members; client-role exclusions + escalation guards |
| `internal/client` | Client provisioning (one-time credential), client list, credential rotation (revokes all sessions) |
| `internal/project` | Projects, milestones, approval state machine, revisions, deliverables, payment status, approval view, client scope enforcement |
| `internal/assignment` | Assignments CRUD (excludes client-role users) |
| `internal/activity` | Denormalized activity feed (org-wide + project-scoped) |
| `internal/audit` | Audit log writer, `RecordDecision` (authz decisions, incl. service-level scope denials with resource identity), `VersionedMetadata` helper |
| `internal/middleware` | Recovery, request ID, logging, security headers, CORS, rate limit, auth (loads `must_change_password`), password gate, org load, org access, RBAC |
| `internal/jwt` | JWT manager + claims |
| `internal/apperrors` | Sentinel errors across layers |
| `internal/response` | JSON envelopes, error mapping, pagination |
| `internal/requestctx` | Typed values carried in request context (user, session, org, role, must_change_password) |
| `internal/slug`, `internal/normalize`, `internal/validation` | Helpers |
| `pkg/config`, `pkg/db`, `pkg/logger`, `pkg/pagination` | Shared infrastructure |
| `migrations/` | Ordered SQL migrations (up/down) — currently 000001–000005 |
| `docs/` | Canonical documentation (see especially `docs/architecture.md`, `docs/api.md`) |

## Data Flow (Confirmed)

```
HTTP client
  → middleware chain (recovery → requestID → logging → securityHeaders → CORS → rate limit)
  → route middleware (auth [loads must_change_password] → RequirePasswordChanged [403 if set, except /v1/me/password]
      → org load → org access → RBAC per-permission)
  → handler (decode/validate)
  → service (business rules, transactions, client-scope checks, audit + activity writes)
  → repository (SQL)
  → PostgreSQL
```

Route pattern example (from `cmd/api/routes.go`):
`GET /v1/orgs/{orgID}/projects/{projectID}` → `RequireAuth(RequirePasswordChanged(LoadOrg(RequireMembership(RequirePermission("project.view")))))`

The password gate (`RequirePasswordChanged`) wraps every authenticated route except `POST /v1/me/password`; cookie-authenticated routes (`/v1/auth/refresh|logout|logout-all`) are exempt by construction (not behind `RequireAuth`). A route-table test enforces this invariant against the single production source of truth.

## Authentication & Authorization (Confirmed)

- **Access token:** short-lived JWT (default 15m) in `Authorization: Bearer`, HMAC-only signing enforced, claims carry `user_id` + `session_id`
- **Session validation per request:** every authenticated request re-checks the session row (not revoked, not expired) against the DB and loads `users.must_change_password` per-request (a stale access token cannot bypass the gate)
- **Refresh token:** rotating, HttpOnly, SameSite=Lax cookie; `Secure` in production; default 720h TTL
- **Rotation & theft detection:** `sessions` table with `token_family_id`; rotating revokes the old row and issues a new one in the same family; presenting a revoked token revokes the whole family (reuse = theft signal)
- **Token storage:** bcrypt (cost 12) authoritative hash + SHA-256 `token_lookup_hash` for deterministic lookup; `FOR UPDATE` row locks on refresh/logout paths; race-safe idempotent logout (`WHERE revoked = false ... RETURNING`)
- **RBAC:** data-driven. `permissions` (atomic keys), `roles` (system template roles with `organization_id IS NULL`: **owner/admin/member/viewer/client**; org-scoped custom roles possible later), `role_permissions`, `memberships` (points at a role, status active/invited/suspended). `RequirePermission` does a single SQL `EXISTS` check; every decision is recorded via `audit.RecordDecision`
- **Client role (narrow, project-scoped):** holds exactly `project.view`, `milestone.view`, `milestone.approve`, `milestone.revision.request`, `activity.project.list`. Never list/org/member/assignment keys. `milestone.approve`/`milestone.revision.request` are additionally restricted at the service layer to the project's assigned client. The owner role holds every key (incl. the two approve keys) but is functionally blocked by the same service-layer check — the service check is authoritative.
- **Escalation guards:** `member.role.update` rejects `client` as current or target role; `member.remove` on a client-role membership → 409 `client_attached_to_project` (unassign flow is the only removal path); assignment endpoints exclude client-role users; `member.list` excludes client-role memberships (query level, count + items)
- **Rate limiting:** in-memory per-IP — global 100/min; login 5/min, register 3/min, refresh 10/min, password change 5/min

## Database (Confirmed)

Base tables: `users`, `organizations`, `permissions`, `roles`, `role_permissions`, `memberships`, `sessions`, `audit_logs`, `authz_decisions`. Layer-1 additions (migration 000005): `milestone_revisions`, `milestone_deliverables`. Conventions:

- UUID PKs (`gen_random_uuid()` via pgcrypto), `TIMESTAMPTZ`, `created_at`/`updated_at` maintained by `update_updated_at_column()` triggers (history tables are append-only: no `updated_at`, no trigger)
- `users`: email OR phone (CHECK constraint), `email_verified`/`phone_verified` columns **exist but have no flow to set them true** (dormant); **`must_change_password BOOLEAN NOT NULL DEFAULT FALSE`** (forced rotation of one-time credentials)
- `projects`: **`client_id UUID REFERENCES users(id) ON DELETE SET NULL`** (single client per project MVP; join table is the later escape hatch), **`revision_limit INTEGER NULL`** (project default)
- `milestones`: `status` enum `pending|in_progress|awaiting_approval|completed|blocked|cancelled|approved|changes_requested` + mirrored CHECK; **`payment_status`** enum `unpaid|partial|paid` (default unpaid) + CHECK; **`revision_count INT NOT NULL DEFAULT 0`**; **`revision_limit INT NULL`** (per-milestone override; NULL = unlimited)
- `milestone_revisions`: append-only submission history (one row per submission round; `UNIQUE (milestone_id, revision_number)`; `submitted_by` FK RESTRICT)
- `milestone_deliverables`: link-based deliverables (url CHECK http/https prefix, title ≤200, description ≤2000, `submitted_by` FK RESTRICT); deliberately NOT project-scoped (reached only via milestones; service validates milestone→project→org before read/write)
- `memberships`: unique per (user, org); `role_id` FK RESTRICT
- `authz_decisions.permission_key` denormalized (not FK); **resource_type/resource_id now populated for service-level scope denials** (client outside project scope)
- Indexes: `audit_logs(entity_type, entity_id, created_at DESC)` (per-entity history), `projects(organization_id, client_id)`, `milestone_revisions(organization_id, milestone_id)`, `milestone_deliverables(organization_id, milestone_id)` + `(milestone_id, submitted_at DESC)`
- Transactions: multi-table mutations run in a single `*sql.Tx` via `db.WithTransaction`; slug-retry uses savepoints (PG aborts a transaction on unique violation)
- State-machine pattern: org-scoped `FOR UPDATE` milestone read + project read (project also supplies `client_id` + archived/cancelled guard) → status-guarded `UPDATE ... WHERE status = <expected>` (RowsAffected check) → versioned audit + activity in the same tx; `FOR UPDATE` serializes concurrent actors so the second re-reads committed state (idempotent approve no-ops; non-idempotent changes-requested 409s)

## External Systems

- **None currently.** No email provider, no object storage, no payment provider, no SMS.
- **Proposed (see ROADMAP/OPEN_QUESTIONS):** email delivery (buy — Resend/SES/Postmark class); object storage later (link-based deliverables in place); payment provider at Layer 3 (Paystack/Flutterwave class per confirmed geography).

## Trust Boundaries (Confirmed)

1. Public routes: register, login, refresh (rate-limited)
2. `RequireAuth`: JWT + live session check → establishes identity
3. `RequirePasswordChanged`: `users.must_change_password` gate → 403 `password_change_required` until the one-time credential is rotated (sole exception `POST /v1/me/password`, which clears the flag transactionally)
4. `LoadOrg` + `RequireMembership`: resolves org from path, verifies active membership → establishes tenant context
5. `RequirePermission`: role → permission check → establishes capability (role keys)
6. **Client project scope (service layer):** for `requestctx.Role == "client"`, reads/actions resolve only when `projects.client_id == actor`; anything else → 404 `project_not_found` (no existence leak), denial recorded in `authz_decisions` with resource identity. This is the resource-dimension enforcement the permission system lacks.
7. Query-level `organization_id` scoping: final backstop for tenant isolation. **(Hole found in 2026-08-13 audit — project/milestone read paths were unscoped — FIXED 2026-08-13; regression-tested.)**

## Milestone Approval State Machine (Confirmed, built 2026-08-14)

Statuses: `pending`, `in_progress`, `awaiting_approval`, `approved`, `changes_requested`, `completed`, `blocked`, `cancelled`. Action endpoints (action-only statuses are unreachable via the generic PATCH):

| From | To | Route / permission | Notes |
|---|---|---|---|
| pending | in_progress | `PATCH .../status` (`milestone.status.update`, staff) | |
| pending | cancelled | `PATCH .../status` (staff) | escape hatch from every non-terminal state |
| in_progress | pending / blocked / cancelled | `PATCH .../status` (staff) | |
| blocked | in_progress / cancelled | `PATCH .../status` (staff) | |
| in_progress, changes_requested | awaiting_approval | `POST .../submit` (`milestone.submit`, staff) | creates `milestone_revisions` row + increments `revision_count`; 400 `project_has_no_client` / `deliverable_required`; idempotent when already awaiting_approval |
| awaiting_approval | approved | `POST .../approve` (`milestone.approve`, client-only) | idempotent; actor must be the project's client |
| awaiting_approval | changes_requested | `POST .../changes-requested` (`milestone.revision.request`, client-only) | `notes` required 3–2000; NOT idempotent (409 `milestone_not_awaiting_approval` on stale) |
| awaiting_approval, changes_requested | blocked / cancelled | `PATCH .../status` (staff) | |
| approved | completed | `PATCH .../status` (staff) | stamps `completed_at`; only path to completion |

- `awaiting_approval → completed` is **forbidden** (client sign-off is the wedge; cancel is the escape hatch)
- All four state-machine actions are blocked (400 `invalid_status_transition`) when the project is `archived`/`cancelled`
- `completed`/`cancelled` are terminal; every transition writes a versioned audit row (that row IS the status-transition history) + activity entry, in the same tx
- Revision semantics: `revision_count` = submission rounds (initial = 1); `limit_reached` computed at read (`limit = COALESCE(m.revision_limit, p.revision_limit)`; flag = limit set AND `revision_count >= limit`); hidden from client surfaces

## Reliability & Scalability (Mostly Unknown)

- **Reliability requirements: not defined** (no SLOs, no load testing, no capacity estimates) — Unknown
- **Scalability:** single instance today; in-memory rate limiting is per-process (sticky/load-balanced multi-instance would break limits); Redis rate limiting deferred. Horizontal scaling, K8s, observability are deferred. — Confirmed as current reality; scaling plans Unknown
- Graceful shutdown + `/v1/ready` + `/v1/live` probes exist — Confirmed

## Product-Direction Architecture Notes (future)

- **Email delivery** infrastructure — proposed (buy: Resend/SES/Postmark class); foundation for invites, password reset, verification
- **AI layer** (Layer 4) — deliberately not designed; will consume the audit/activity substrate. **The substrate is now AI-compatible**: activity metadata flows through, Layer-1 events use versioned metadata (`{"schema_version":1,"before","after","reason"}`), the milestone state machine writes an audit row per transition (status-transition history), `milestone_revisions` carries revision history, and `audit_logs(entity_type, entity_id, created_at DESC)` backs per-entity history queries. No event bus/outbox/Kafka planned.
- **Payment layer** (Layer 3) — milestone payment status is display-only in Layer 1; invoicing, WHT (Nigeria 5%/10%), Paystack/Flutterwave, escrow/payouts are future
- The existing audit/activity/RBAC substrate is a strong fit for the workflow product — Confirmed assessment; not yet validated against real product requirements

## Known Gaps & Bugs (current)

- **`docs/deployment.md` rollback example is stale** — lists only 000003/000002/000001 down migrations; 000004/000005 down files now exist (one-line fix)
- **`docs/roadmap.md` is legacy** — old generic-PM framing; needs reconciliation (OPEN_QUESTIONS Q9)
- **`project.status.update` permission is seeded but unused** — status changes flow through `PATCH /projects/{id}` with `project.update` (pre-existing, minor)
- **No API endpoint sets `revision_limit`** — schema + read-side fully wired; admin setter is a small follow-up (default = unlimited)
- **Provisioned-but-never-assigned clients cannot be removed** — `member.remove` rejects all client-role memberships (409); unassign requires a project link; no API removal path for a client with no project (operational: persists in `client.list` until DB cleanup)
- **Client `milestone.list` on a non-owned project returns 403, not 404** — clients lack the key entirely (RBAC denies first); no existence leak; accepted deviation (narrow key set by design)
- **Rate-limit bypass risk (pre-existing):** `getClientIP` trusts `X-Forwarded-For` unconditionally — limits bypassable if exposed without a sanitizing proxy
- **`authz_decisions` grows unbounded** — cleanup SQL exists in the Makefile (`authz-decisions-cleanup`, 90-day window); needs a cron at deploy
- **No CI** — integration tests require live PostgreSQL; nothing runs them automatically (deferred to deploy by founder decision). A CI gate must NOT run `-race` on the whole suite: bcrypt-cost-12 tests exceed the 5s DB timeout under race instrumentation (pre-existing, environmental)
- **Boundary-hardening coverage gaps (pre-existing, minor)** — dedicated HTTP cross-org project GET/PATCH tests and the live concurrency-lock test remain useful additions; service/repository coverage exists

## Unknowns

- Whether file uploads (object storage) are needed or links suffice for deliverables (unvalidated with real clients)
- Frontend architecture (none exists; decision pending — see OPEN_QUESTIONS.md Q5)
- Deployment target and operations model (deferred to devops agent at deploy time)
- Real-agency validation of the wedge (workflow clarity → cash flow) — the core business assumption, untested
