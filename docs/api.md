# Egentop-Core API Reference

Base URL: `http://localhost:8080` (see `APP_PORT`).

All endpoints return JSON. Versioned under `/v1`.

## Conventions

### Envelope

Successful responses:

```json
{
  "success": true,
  "message": "project created",
  "data": { ... }
}
```

Error responses:

```json
{
  "success": false,
  "error": {
    "code": "validation_error",
    "message": "validation failed"
  }
}
```

Validation errors include a `fields` object mapping field names to messages:

```json
{
  "success": false,
  "error": {
    "code": "validation_error",
    "message": "validation failed",
    "fields": { "name": "name is required" }
  }
}
```

Request bodies are limited to **1 MiB** by middleware (`MaxBytesReader`). A
larger body fails the handler's JSON decode, and the middleware returns
`413 payload_too_large` (JSON envelope) in place of the decode error.

### Pagination

List endpoints accept `page` and `limit` query parameters. The default page is
`1`; the default limit is `20`. The `data` field of a paginated response has
this shape:

```json
{
  "items": [ ... ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total": 42
  }
}
```

### Authentication

- Bearer token: `Authorization: Bearer <access_token>` for every protected route.
- Refresh token: `refresh_token` HttpOnly cookie, set on register/login and
  rotated on every refresh.
- `POST /v1/auth/refresh`, `POST /v1/auth/logout` and `POST /v1/auth/logout-all`
  authenticate solely via the `refresh_token` cookie and work with an expired
  or absent access token.
- `GET /v1/me`, `GET /v1/orgs` and `POST /v1/orgs` require only a valid access
  token. All other `/v1/orgs/{orgID}/...` routes additionally require an active
  membership in that organization and a role holding the required permission
  (see [Roles & Permissions](#roles-and-permissions)).
- Users whose `users.must_change_password` flag is set (clients provisioned
  with a one-time credential) receive `403 password_change_required` on every
  authenticated route except `POST /v1/me/password`. The cookie-authenticated
  routes (`refresh`, `logout`, `logout-all`) are exempt. The gate lifts once
  the password is changed.

### Rate Limiting

| Endpoint | Limit |
|----------|-------|
| `POST /v1/auth/register` | 3 / minute |
| `POST /v1/auth/login` | 5 / minute |
| `POST /v1/auth/refresh` | 10 / minute |
| `POST /v1/me/password` | 5 / minute |
| Everything else | 100 / minute |

Exceeding a limit returns `429 Too Many Requests` with a **plain-text** body
(`too many requests`) — the limiter runs in middleware, before the JSON error
envelope, so the `rate_limited` JSON code in the error handler is not produced
by any current route.

The limit is keyed on the request's **real IP**: the `X-Real-IP` header when it
parses as a valid IP, otherwise the socket peer (`RemoteAddr`). `X-Forwarded-For`
is **never** trusted. Because `X-Real-IP` is client-supplied at the socket, the
application is only safe behind a proxy that overwrites both headers with the
IP it actually observed (see [docs/security.md](security.md) and
[docs/deployment.md](deployment.md)).

---

## Health

### GET /v1/health
Liveness. Returns `200` always.

### GET /v1/live
Liveness. Returns `200`.

### GET /v1/ready
Readiness. Pings the database.
- `200` when the database is reachable
- `503` with code `service_unavailable` when it is not

---

## Authentication

### POST /v1/auth/register
Creates a new user account. Also creates a default organization named
`<first_name>'s Organization` and makes the new user its `owner`. Sets the
`refresh_token` cookie and returns an access token.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `email` | string | optional, valid email, max 100 |
| `phone` | string | optional, valid Nigerian phone number |
| `password` | string | required, min 8, max 72 |
| `first_name` | string | required, min 2, max 50 |
| `last_name` | string | required, min 2, max 50 |

At least one of `email` or `phone` is required.

Response `200`:

```json
{
  "success": true,
  "message": "registration successful",
  "data": { "access_token": "..." }
}
```

Errors: `400` `validation_error` / `invalid_request_body` / `invalid_identifier`
(both identifiers missing), `409` `email_already_exists` / `phone_already_exists`.

### POST /v1/auth/login
Authenticates a user by email or phone. Sets the `refresh_token` cookie.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `identifier` | string | required, email or phone |
| `password` | string | required, min 4, max 72 |

Response `200`:

```json
{
  "success": true,
  "message": "login successful",
  "data": { "access_token": "..." }
}
```

Every failed login is indistinguishable: an unknown identifier, a suspended
account, and a wrong password all return `401 invalid_credentials` (no account
enumeration via login; registration keeps its own `409` responses by accepted
trade-off). Login never returns `403 forbidden` — there is no producing code
path for it.

Errors: `400` `validation_error` / `invalid_request_body` / `invalid_identifier`,
`401` `invalid_credentials`.

### POST /v1/auth/refresh
Rotates the refresh token from the `refresh_token` cookie and returns a new
access token. Authenticated solely by the `refresh_token` cookie; works even
when the access token is expired or absent.

Response `200`:

```json
{
  "success": true,
  "message": "login successful",
  "data": { "access_token": "..." }
}
```

Errors: `401` `invalid_token` / `session_revoked`.

### POST /v1/auth/logout
Revokes the session referenced by the `refresh_token` cookie and clears the
cookie. Authenticated solely by the cookie; works when the access token is
expired or absent. Idempotent: a missing, expired, or not-active cookie still
returns `204` and clears the cookie.

Response `204`.

Errors: none (idempotent); DB failures return `500` `internal_server_error`.

### POST /v1/auth/logout-all
Revokes every active session for the user identified by the `refresh_token`
cookie and clears the cookie. The user is resolved from the cookie's session.
Idempotent: possession of the refresh cookie is sufficient to revoke all
sessions — by design, consistent with industry practice.

Response `204`.

Errors: none (idempotent); DB failures return `500` `internal_server_error`.

---

## Current User

### GET /v1/me
Returns the authenticated user's profile.

Response `200`:

```json
{
  "success": true,
  "message": "user fetched",
  "data": {
    "id": "uuid",
    "email": "user@example.com",
    "phone": "+2348000000000",
    "first_name": "Ada",
    "last_name": "Okafor",
    "status": "active",
    "email_verified": false,
    "phone_verified": false,
    "created_at": "2026-08-11T10:00:00Z",
    "updated_at": "2026-08-11T10:00:00Z"
  }
}
```

### PATCH /v1/me
Updates the authenticated user's display name.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `first_name` | string | required, min 2, max 50 |
| `last_name` | string | required, min 2, max 50 |

Response `200` with the updated profile (same shape as `GET /v1/me`).

### POST /v1/me/password
Changes the authenticated user's password and revokes all other sessions.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `current_password` | string | required, min 8, max 72 |
| `new_password` | string | required, min 8, max 72 |

Response `200`:

```json
{
  "success": true,
  "message": "password changed",
  "data": { "message": "password changed; other sessions have been logged out" }
}
```

Errors: `400` `validation_error` / `weak_password` (new password identical to
the current one), `401` `invalid_password`.

---

## Organizations

### POST /v1/orgs
Creates an organization and makes the authenticated user its `owner`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `name` | string | required, min 2, max 50 |

Response `201`:

```json
{
  "success": true,
  "message": "organization created",
  "data": { "organization_id": "uuid" }
}
```

Errors: `400` `validation_error`, `409` `organization_slug_exists`.

### GET /v1/orgs
Lists the organizations the authenticated user is a member of. Paginated.

Response `200`: paginated list of membership objects:

```json
{
  "items": [
    {
      "id": "uuid",
      "user_id": "uuid",
      "organization_id": "uuid",
      "role_id": "uuid",
      "role": "owner",
      "status": "active",
      "joined_at": "2026-08-11T10:00:00Z"
    }
  ],
  "pagination": { "page": 1, "limit": 20, "total": 1 }
}
```

### GET /v1/orgs/{orgID}
Returns organization details. Requires membership in the organization.

Response `200`:

```json
{
  "success": true,
  "message": "organization fetched",
  "data": {
    "id": "uuid",
    "name": "Acme",
    "slug": "acme",
    "status": "active",
    "created_at": "2026-08-11T10:00:00Z",
    "updated_at": "2026-08-11T10:00:00Z"
  }
}
```

### PATCH /v1/orgs/{orgID}
Renames an organization. Permission: `org.update`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `name` | string | required, min 2, max 50 |

Response `200` with the updated organization (same shape as `GET /v1/orgs/{orgID}`).

---

## Memberships

### GET /v1/orgs/{orgID}/members
Lists members of an organization. Permission: `member.list`. Paginated.

Response `200`:

```json
{
  "items": [
    {
      "id": "uuid",
      "user_id": "uuid",
      "organization_id": "uuid",
      "role_id": "uuid",
      "role": "member",
      "status": "active",
      "joined_at": "2026-08-11T10:00:00Z"
    }
  ],
  "pagination": { "page": 1, "limit": 20, "total": 1 }
}
```

### POST /v1/orgs/{orgID}/members
Adds an existing user to the organization by user ID. Permission: `member.invite`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `user_id` | string | required, UUID |
| `role` | string | required, one of `owner`, `admin`, `member`, `viewer` |

Response `201`.

Errors: `400` `validation_error`, `403` `forbidden` (non-owner granting
`owner`), `404` `user_not_found`, `409` `already_member`.

### POST /v1/orgs/{orgID}/members/invite
Invites an existing user by email. The invitation is created with status
`invited`; the target user must already exist (`404 user_not_found`
otherwise — invitations for non-existent users are not recorded). Permission:
`member.invite`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `email` | string | required, valid email |
| `role` | string | required, one of `owner`, `admin`, `member`, `viewer` |

Response `201`.

Errors: `400` `validation_error`, `403` `forbidden` (non-owner granting
`owner`), `404` `user_not_found`, `409` `already_member` / `invitation_pending`.

### PATCH /v1/orgs/{orgID}/members/{userID}
Changes a member's role. Permission: `member.role.update`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `role` | string | required, one of `owner`, `admin`, `member`, `viewer` |

Response `200`.

Errors: `400` `validation_error`, `403` `forbidden`, `404` `membership_not_found`.

### DELETE /v1/orgs/{orgID}/members/{userID}
Removes a member from the organization. The owner cannot be removed. Permission:
`member.remove`.

Response `200`.

Errors: `403` `forbidden`, `404` `membership_not_found`.

Client-role memberships are **not** listed here and **cannot** be removed or
re-role'd through the membership endpoints:

- `GET /v1/orgs/{orgID}/members` excludes client-role memberships.
- `PATCH /v1/orgs/{orgID}/members/{userID}` rejects `client` as the target
  role (the DTO `oneof` validation returns `400 validation_error`) and rejects
  any membership that currently holds the client role (`403 forbidden`).
- `DELETE /v1/orgs/{orgID}/members/{userID}` on a client-role membership
  returns `409 client_attached_to_project` — clients are removed exclusively
  through the project unassign flow.

---

## Clients

Clients are modeled as real users with a `client`-role membership (there is no
separate clients table). A client's access is **project-scoped**: they can see
only the projects assigned to them and the milestones, deliverables, payment
status and activity of those projects.

### POST /v1/orgs/{orgID}/clients
Provisions a client account. Permission: `client.provision`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `email` | string | optional, valid email, max 100 |
| `phone` | string | optional, valid Nigerian phone number |
| `first_name` | string | required only when creating a new user, min 2, max 50 |
| `last_name` | string | required only when creating a new user, min 2, max 50 |

At least one of `email` or `phone` is required.

Behavior:

- **Existing user, not a member** — the user is reused and an active
  `client` membership is created. No credential is issued: the user's existing
  password remains authoritative (`credential_issued: false`).
- **Existing user, already a member** — `409 already_member` (a user can hold
  only one membership per organization).
- **No matching user** — a new user is created with a random one-time
  credential, `must_change_password = true`, and an active `client`
  membership. The credential is returned **exactly once** in the response and
  must be rotated via `POST /v1/me/password` before the client can do anything
  else (see the password gate above).

Provisioning never creates a default organization and never registers a
session.

Response `201` (new user created):

```json
{
  "success": true,
  "message": "client provisioned",
  "data": {
    "client_id": "uuid",
    "email": "client@example.com",
    "credential_issued": true,
    "one_time_password": "k4M9xQ2vT7pW3bN8"
  }
}
```

Response `201` (existing user reused — no credential):

```json
{
  "success": true,
  "message": "client provisioned",
  "data": {
    "client_id": "uuid",
    "email": "user@example.com",
    "credential_issued": false
  }
}
```

Errors: `400` `validation_error`, `409` `already_member` /
`email_already_exists` / `phone_already_exists`.

### GET /v1/orgs/{orgID}/clients
Lists the organization's clients (client-role memberships only; staff
memberships are excluded), newest first. Permission: `client.list`. Paginated.

Response `200`:

```json
{
  "items": [
    {
      "user_id": "uuid",
      "email": "client@example.com",
      "phone": null,
      "first_name": "Ada",
      "last_name": "Okafor",
      "joined_at": "2026-08-11T10:00:00Z"
    }
  ],
  "pagination": { "page": 1, "limit": 20, "total": 1 }
}
```

### POST /v1/orgs/{orgID}/clients/{userID}/reset-credential
Rotates a client's one-time credential. Permission: `client.provision`.

The target must hold an active `client` membership in the organization (else
`404 client_not_found`). The operation:

- replaces the password hash with a fresh one-time credential,
- sets `must_change_password = true` (the gate re-arms),
- revokes **all** of the client's sessions — a client still logged in with the
  old credential loses access immediately.

The new credential is returned exactly once.

Response `200`:

```json
{
  "success": true,
  "message": "client credential reset",
  "data": {
    "client_id": "uuid",
    "email": "client@example.com",
    "credential_issued": true,
    "one_time_password": "w6Zb2Dn5Hq8Lx3Mv"
  }
}
```

Errors: `404` `client_not_found`.

### DELETE /v1/orgs/{orgID}/clients/{userID}
Removes a provisioned-but-unassigned client's membership from the
organization. Permission: `client.provision`.

The target must hold an active `client` membership in the organization (else
`404 client_not_found`). Behavior:

- The **membership** row is deleted; the `users` row is never deleted.
- The removal is rejected with `409 client_attached_to_project` when the
  client is still the assigned client of any project in the organization —
  unassign those projects first (`PUT .../projects/{projectID}/client` with
  `null`).
- The membership row is locked `FOR UPDATE` during the check so a concurrent
  project assignment cannot race the removal (the assignment either commits
  first and blocks the removal with 409, or the removal commits first and the
  assignment aborts with `404 client_not_found`).

Response `200` with a success envelope (the `data` key is omitted when nil).

Errors: `403` `forbidden` (actor lacks `client.provision`), `404`
`client_not_found`, `409` `client_attached_to_project`.

---

## Projects

### POST /v1/orgs/{orgID}/projects
Creates a project. Permission: `project.create`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `name` | string | required, min 3, max 120 |
| `description` | string | optional, max 2000 |
| `priority` | string | optional, `low`, `medium` or `high` |
| `due_date` | string (RFC 3339) | optional; must be **today or later** (UTC date) when present |

**Due-date rule (§14.2):** `due_date` may be absent (no due date), but when
present its UTC date component must not be before today's UTC date. A date
equal to today passes at any clock time (date-only comparison). A violation
returns `400 validation_error` with `fields.DueDate` = `"due date can't be in
the past"` and **no project row is created**.

Response `201` with the created project:

```json
{
  "success": true,
  "message": "project created",
  "data": {
    "id": "uuid",
    "organization_id": "uuid",
    "created_by": "uuid",
    "name": "Website Redesign",
    "description": "Refresh the marketing site",
    "status": "draft",
    "priority": "medium",
    "due_date": "2026-12-31T00:00:00Z",
    "client_id": null,
    "created_at": "2026-08-11T10:00:00Z",
    "updated_at": "2026-08-11T10:00:00Z"
  }
}
```

`status` is one of `draft`, `active`, `completed`, `archived`, `cancelled`.
`priority` is one of `low`, `medium`, `high`. `client_id` is the project's
assigned client (set via `PUT .../projects/{projectID}/client`), or `null`
when no client is assigned. Project-level fields such as `revision_limit` are
not exposed on the project payload; the effective revision limit surfaces on
milestones (see the Milestones section).

### GET /v1/orgs/{orgID}/projects
Lists projects in the organization. Permission: `project.list`. Paginated.

Query parameters:

| Parameter | Type | Meaning |
|-----------|------|---------|
| `page` | integer | page number (1-based) |
| `limit` | integer | items per page |
| `include_cancelled` | string | `true` opts cancelled projects back in ("Show closed"); any other value (or absent) excludes them |

**List filtering (§14.2.4):** the default list includes draft, active,
completed and archived projects; **cancelled** projects are excluded from
both `items` and `pagination.total` unless `include_cancelled=true`.
**Soft-deleted** projects are never included, by any parameter, and are also
excluded from `total`.

Response `200`: paginated list of project objects (shape above).

### GET /v1/orgs/{orgID}/projects/{projectID}
Returns a single project. Permission: `project.view`.

Client-role actors can only read the projects assigned to them; any other
project resolves to `404 project_not_found` (existence never leaks).

Response `200` with a project object.

Errors: `404` `project_not_found`.

### PATCH /v1/orgs/{orgID}/projects/{projectID}
Updates project metadata and/or status. Permission: `project.update`.

Request body (all fields optional):

| Field | Type | Rules |
|-------|------|-------|
| `name` | string | min 3, max 120 |
| `description` | string | max 2000 |
| `priority` | string | `low`, `medium` or `high` |
| `status` | string | `draft`, `active`, `completed`, `archived`, `cancelled` |
| `due_date` | string (RFC 3339) or `null` | must be **today or later** (UTC date) when a value is present; `null` **clears** the date; absent leaves it unchanged |

**Freeze contract (§14.1):** archived and cancelled projects are read-only.
Any PATCH that would change a field or status on them returns
`400 invalid_status_transition` and mutates nothing, with two carve-outs:
a status-only `{"status":"active"}` on an **archived** project is the
**restore** transition (§14.1.1) and is allowed (the restore emits its own
`project.restored` audit + activity events); **cancelled** is terminal and
rejects every PATCH, including restore. Completed projects stay fully
mutable (not frozen).

**Due-date rule (§14.2):** when `due_date` is a value it must not be in the
past (UTC date comparison, same rule as create); a violation returns
`400 validation_error` with `fields.DueDate` and leaves every field —
including the previous due date — unchanged. Precedence: a soft-deleted
project resolves `404 project_not_found` first; a frozen project resolves
`400 invalid_status_transition` before the due-date rule.

Response `200` with the updated project.

Errors: `400` `validation_error` / `invalid_status_transition` /
`invalid_project_priority`, `404` `project_not_found`.

### DELETE /v1/orgs/{orgID}/projects/{projectID}
Soft-deletes a project. Permission: `project.update` (owner).

**Soft delete (§14.1):** the project row is preserved with `deleted_at` set,
so the audit trail and the org activity feed keep the project's history. The
delete works from **every** status (draft, active, completed, archived,
cancelled) with identical behavior. After deletion every project-scoped read
and mutation resolves `404 project_not_found` (no existence leak — it is
indistinguishable from a never-existing project), the project disappears
from every list (items and total), and a second delete returns `404`.
Deleting a project does **not** prune the client's membership (the row keeps
`client_id` for the audit record). Restore-from-delete is an explicit
non-goal this slice: deleted is final, history preserved.

Request body: none.

Response `200` with a success envelope (the `data` key is omitted when nil):

```json
{
  "success": true,
  "message": "project deleted"
}
```

Errors: `403` `forbidden` (role without `project.update`, a client, or an
authenticated member of another org — org-scoped routes return `403` for a
member of a different org: accepted deviation, consistent with the MEDIUM-4
org-existence oracle; revisit pre-launch), `404` `project_not_found` (missing,
another org's project reached through the actor's own org scope, or already
deleted).

### PUT /v1/orgs/{orgID}/projects/{projectID}/client
Assigns, reassigns, or unassigns the project's client. Permission:
`project.client.assign`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `client_id` | string (UUID) or `null` | optional; `null` unassigns |

Behavior:

- **Assign** — the user must hold an active `client` membership in the
  organization, else `404 client_not_found`.
- **Reassign** — the previous client loses access to this project immediately
  (the per-request scope check is authoritative).
- **Unassign** (`null`) — removes the project's client. Unassign and reassign
  both prune the displaced client's membership once they are no longer the
  client of any other project in the organization.
- No-op requests (no client / same client) succeed without changes.
- **Frozen projects (§14.1, AC-LC-7):** assigning/reassigning/unassigning on
  an archived or cancelled project returns `400 invalid_status_transition`.

Response `200` with the updated project.

Errors: `400` `validation_error` / `invalid_status_transition`, `404`
`project_not_found` / `client_not_found`.

### PATCH /v1/orgs/{orgID}/projects/{projectID}/revision-limit
Sets or clears the project-level revision limit (the default every milestone
falls back to). Permission: `project.update` (owner). Agency-only —
the limit fields never appear on client-facing surfaces.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `revision_limit` | integer or `null` | optional; `null`/absent = unlimited, must be ≥ 1 when set |

The effective limit per milestone is computed at read time
(`COALESCE(milestone.revision_limit, project.revision_limit)`), so setting or
clearing this value immediately affects every milestone of the project that
has no per-milestone override. The change is audited
(`project.revision_limit_changed`, before/after) and written to the activity
feed.

Response `200` with the updated project (the project payload itself never
exposes `revision_limit`).

**Frozen projects (§14.1, AC-LC-6):** setting or clearing the limit on an
archived or cancelled project returns `400 invalid_status_transition`.

Errors: `400` `validation_error` (including `revision_limit` < 1) /
`invalid_status_transition`, `404` `project_not_found`.

---

## Milestones

### POST /v1/orgs/{orgID}/projects/{projectID}/milestones
Creates a milestone. Permission: `milestone.create`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `title` | string | required, min 3, max 120 |
| `description` | string | optional, max 2000 |
| `due_date` | string (RFC 3339) | optional; must be **today or later** (UTC date) when present |

**Due-date rule (§14.2):** same rule as projects — `due_date` may be absent,
but a present value must not be in the past (UTC date). A violation returns
`400 validation_error` with `fields.DueDate` and **no milestone row is
created**.

**Frozen projects (§14.1, AC-LC-3):** creating a milestone on an archived or
cancelled project returns `400 invalid_status_transition`.

Response `201` with the created milestone:

```json
{
  "success": true,
  "message": "milestone created",
  "data": {
    "id": "uuid",
    "organization_id": "uuid",
    "project_id": "uuid",
    "created_by": "uuid",
    "title": "Design Phase",
    "description": "Finalize the design",
    "status": "pending",
    "due_date": "2026-10-31T00:00:00Z",
    "position": 1,
    "completed_at": null,
    "revision_count": 0,
    "limit_reached": false,
    "payment_status": "unpaid",
    "created_at": "2026-08-11T10:00:00Z",
    "updated_at": "2026-08-11T10:00:00Z"
  }
}
```

`status` is one of `pending`, `in_progress`, `awaiting_approval`, `completed`,
`blocked`, `cancelled`, `approved`, `changes_requested`. `payment_status` is
one of `unpaid`, `partial`, `paid` (display-only; no money movement).
`revision_count` is the number of submission rounds (initial submission = 1,
each resubmission = +1). A newly created milestone has no limit configured, so
the create response carries `limit_reached: false` and omits `revision_limit`
(the field is nil at creation; the effective limit is computed at read time for
staff-facing responses).

### GET /v1/orgs/{orgID}/projects/{projectID}/milestones
Lists milestones for a project. Permission: `milestone.list`. Paginated.
Clients do not hold `milestone.list` (a client calling this route directly is
denied `403 forbidden` by RBAC before the service layer); they reach milestones
through the approval view and milestone detail. The service-layer scope rule
still applies defensively: a client-role actor resolves only their own project
(else `404 project_not_found`). List items have the milestone shape above but
do not embed `deliverables`; staff additionally see `revision_limit` (the
effective limit: the milestone's override, else the project default, else
`null` = unlimited) and `limit_reached` (`revision_count >= revision_limit`).

### GET /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}
Returns a single milestone with its `deliverables` embedded. Permission:
`milestone.view`.

Staff receive the full milestone payload, including the agency-facing
`revision_limit` and `limit_reached` fields. Client-role actors receive the
same payload **without** `revision_limit` / `limit_reached` (the approval
surface), and only for milestones of their own project.

Errors: `404` `milestone_not_found` / `project_not_found` (client actor
outside their project).

### PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}
Updates milestone **metadata** (title, description, due date, position) only.
Permission: `milestone.update`. Status changes go exclusively through the
state-machine endpoints below (`submit` / `approve` / `changes-requested` /
`PATCH .../status`).

Request body (all fields optional):

| Field | Type | Rules |
|-------|------|-------|
| `title` | string | min 3, max 120 |
| `description` | string | max 2000 |
| `due_date` | string (RFC 3339) or `null` | must be **today or later** (UTC date) when a value is present; `null` **clears** the date; absent leaves it unchanged |
| `position` | integer | optional |

**Due-date rule (§14.2):** a past value returns `400 validation_error` with
`fields.DueDate` and leaves the previous date and every other field
unchanged. **Frozen projects (§14.1, AC-LC-4):** editing metadata on an
archived or cancelled project returns `400 invalid_status_transition`.

Response `200` with the updated milestone.

Errors: `400` `validation_error` / `invalid_status_transition`, `404`
`milestone_not_found` / `project_not_found`.

### POST /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/submit
Submits a milestone for client approval. Permission: `milestone.submit`
(staff: owner, admin, member).

Valid from `in_progress` or `changes_requested`. Creates a `milestone_revisions`
row, increments `revision_count` by 1, and moves the milestone to
`awaiting_approval`. Idempotent: an already-`awaiting_approval` milestone
returns success with no duplicate revision row or counter increment.
Blocked when the project is `archived` or `cancelled`.

Request body: none.

Response `200` with the updated milestone.

Errors: `400` `project_has_no_client` (the project has no assigned client),
`400` `deliverable_required` (the milestone has no deliverables),
`400` `invalid_status_transition` (invalid source state or blocked project),
`404` `milestone_not_found`.

### POST /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/approve
Approves a submitted milestone — the client's sign-off. Permission:
`milestone.approve`. The service requires the actor to be the project's
assigned client regardless of RBAC grants, so in practice this action is
client-only (the `client` role is seeded with the key; the owner seed also
carries it but is blocked by the same service check).

Valid only from `awaiting_approval`. Idempotent: an already-`approved`
milestone returns success with no duplicate events.

Request body: none.

Response `200` with the updated milestone.

Errors: `404` `project_not_found` (actor is not the project's client),
`409` `milestone_not_awaiting_approval` (stale state),
`400` `invalid_status_transition` (project archived/cancelled).

### POST /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/changes-requested
Requests changes on a submitted milestone. Permission:
`milestone.revision.request`. As with `approve`, the service requires the
actor to be the project's assigned client regardless of RBAC grants, so in
practice this action is client-only.

Valid only from `awaiting_approval`; moves the milestone to
`changes_requested`. **Not** idempotent: a stale request returns
`409 milestone_not_awaiting_approval`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `notes` | string | required, min 3, max 2000 |

Response `200` with the updated milestone.

Errors: `404` `project_not_found` (actor is not the project's client),
`409` `milestone_not_awaiting_approval`,
`400` `invalid_status_transition` (project archived/cancelled).

### PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/status
Generic staff status transition. Permission: `milestone.status.update`
(owner, admin).

The action-only statuses (`awaiting_approval`, `approved`,
`changes_requested`) are reached exclusively through `submit` / `approve` /
`changes-requested` and are rejected here with a field error. Blocked when the
project is `archived` or `cancelled`. `approved → completed` stamps
`completed_at`. Same-state requests succeed without a transition event.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `status` | string | required, one of `pending`, `in_progress`, `completed`, `blocked`, `cancelled` |

Response `200` with the updated milestone.

Errors: `400` `validation_error` / `invalid_status_transition`,
`404` `milestone_not_found`.

Allowed transitions:

| Current | Allowed targets |
|---------|-----------------|
| `pending` | `in_progress`, `cancelled` |
| `in_progress` | `pending`, `blocked`, `cancelled` |
| `blocked` | `in_progress`, `cancelled` |
| `awaiting_approval` | `blocked`, `cancelled` (escape hatch; never `completed`) |
| `changes_requested` | `in_progress`, `blocked`, `cancelled` |
| `approved` | `completed` |
| `completed` / `cancelled` | — (terminal) |

### POST /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/deliverables
Adds a link-based deliverable to a milestone. Permission: `deliverable.submit`
(staff: owner, admin, member). Duplicates are allowed; there is no edit —
delete and re-add instead. Milestones in `completed` or `cancelled` state are
frozen, and deliverables are also frozen on archived/cancelled **projects**
(§14.1, AC-LC-5).

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `url` | string | required, `http://` or `https://`, max 2000 |
| `title` | string | optional, max 200 |
| `description` | string | optional, max 2000 |

Response `201` with the created deliverable:

```json
{
  "success": true,
  "message": "deliverable submitted",
  "data": {
    "id": "uuid",
    "organization_id": "uuid",
    "milestone_id": "uuid",
    "url": "https://figma.com/file/abc",
    "title": "Homepage mockup",
    "description": "Final direction v2",
    "submitted_by": "uuid",
    "submitted_at": "2026-08-11T10:00:00Z"
  }
}
```

Errors: `400` `validation_error` / `invalid_status_transition`,
`404` `milestone_not_found`.

### DELETE /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/deliverables/{deliverableID}
Removes a deliverable. Permission: `deliverable.submit`. Milestones in
`completed` or `cancelled` state are frozen, and deliverables are also frozen
on archived/cancelled **projects** (§14.1, AC-LC-5).

Response `200` with a success envelope (the `data` key is omitted when nil).

Errors: `400` `invalid_status_transition`, `404` `deliverable_not_found` /
`milestone_not_found`.

### PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/payment-status
Updates the display-only payment status. Permission:
`milestone.payment_status.update` (owner, admin). Any-to-any transitions are
allowed and audited; no state restriction applies (a frozen project is the
only block, §14.1 AC-LC-6). Same-status requests succeed without an event.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `status` | string | required, one of `unpaid`, `partial`, `paid` |

Response `200` with the updated milestone.

Errors: `400` `validation_error` / `invalid_status_transition`, `404`
`milestone_not_found` / `project_not_found`.

### PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}/revision-limit
Sets or clears the per-milestone revision-limit override. Permission:
`milestone.update` (owner, admin). Agency-only — the limit fields never appear
on client-facing surfaces.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `revision_limit` | integer or `null` | optional; `null`/absent clears the override (falls back to the project default), must be ≥ 1 when set |

The response carries the resulting **effective** limit (milestone override,
else project default, else `null` = unlimited) re-read inside the same
transaction. The change is audited (`milestone.revision_limit_changed`,
before/after) and written to the activity feed. Frozen projects (§14.1,
AC-LC-6) block the change with `400 invalid_status_transition`.

Response `200` with the updated milestone.

Errors: `400` `validation_error` (including `revision_limit` < 1) /
`invalid_status_transition`, `404` `milestone_not_found` /
`project_not_found`.

---

## Approval View

### GET /v1/orgs/{orgID}/projects/{projectID}/approval
Returns the shared client-facing approval payload — the deep link a client
opens (e.g. from WhatsApp) to review and sign off on a project. Permission:
`milestone.view`.

Client-role actors resolve only their own project (else `404 project_not_found`).
Staff can also use the endpoint. The payload is the project plus every
milestone with its `deliverables`, `payment_status` and `revision_count`;
revision limits and `limit_reached` are deliberately absent.

Response `200`:

```json
{
  "success": true,
  "message": "approval view fetched",
  "data": {
    "project": {
      "id": "uuid",
      "organization_id": "uuid",
      "created_by": "uuid",
      "name": "Website Redesign",
      "description": "Refresh the marketing site",
      "status": "active",
      "priority": "medium",
      "due_date": "2026-12-31T00:00:00Z",
      "client_id": "uuid",
      "created_at": "2026-08-11T10:00:00Z",
      "updated_at": "2026-08-11T10:00:00Z"
    },
    "milestones": [
      {
        "id": "uuid",
        "project_id": "uuid",
        "title": "Design Phase",
        "description": "Finalize the design",
        "status": "awaiting_approval",
        "due_date": "2026-10-31T00:00:00Z",
        "position": 1,
        "revision_count": 2,
        "payment_status": "partial",
        "deliverables": [
          {
            "id": "uuid",
            "organization_id": "uuid",
            "milestone_id": "uuid",
            "url": "https://figma.com/file/abc",
            "title": "Homepage mockup",
            "description": null,
            "submitted_by": "uuid",
            "submitted_at": "2026-08-11T10:00:00Z"
          }
        ],
        "created_at": "2026-08-11T10:00:00Z",
        "updated_at": "2026-08-11T10:00:00Z"
      }
    ]
  }
}
```

Errors: `404` `project_not_found`.

**Lifecycle behavior (§14.1):** a deep link to an **archived** project
resolves `200` view-only (approve / changes-requested invoked anyway return
`400 invalid_status_transition`); a **cancelled** project also resolves
`200` with a read-only payload (the UI renders the "closed" state); a
**soft-deleted** project resolves `404 project_not_found` — indistinguishable
from a never-existing project (no existence leak).

---

## Assignments

### POST /v1/orgs/{orgID}/projects/{projectID}/assignments
Creates an assignment. Permission: `assignment.create`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `milestone_id` | string | required, UUID |
| `assigned_to` | string | required, UUID |

Response `201` with the created assignment:

```json
{
  "success": true,
  "message": "assignment created",
  "data": {
    "id": "uuid",
    "organization_id": "uuid",
    "project_id": "uuid",
    "milestone_id": "uuid",
    "assigned_to": "uuid",
    "assigned_by": "uuid",
    "created_at": "2026-08-11T10:00:00Z"
  }
}
```

The assignee must hold an active **staff** membership in the organization
(client-role memberships are excluded — assignments are a staff concept).
**Frozen projects (§14.1):** creating an assignment on an archived or
cancelled project returns `400 invalid_status_transition`; on a soft-deleted
project the project resolves `404 project_not_found`.

Errors: `400` `validation_error` / `invalid_status_transition`, `404`
`project_not_found` / `milestone_not_found` / `membership_not_found`
(assignee is not an active staff member).

### GET /v1/orgs/{orgID}/projects/{projectID}/assignments
Lists assignments for a project. Permission: `assignment.list`. Paginated.

### GET /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}
Returns a single assignment. Permission: `assignment.view`.

Errors: `404` `assignment_not_found` / `project_not_found` (missing,
cross-org, or soft-deleted project).

### PATCH /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}
Reassigns an assignment to a different user. Permission: `assignment.update`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `assigned_to` | string | required, UUID |

Response `200` with the updated assignment.

**Frozen projects (§14.1):** reassigning on an archived or cancelled project
returns `400 invalid_status_transition`; on a soft-deleted project the
project resolves `404 project_not_found`.

Errors: `400` `validation_error` / `invalid_status_transition`, `404`
`assignment_not_found` / `membership_not_found` (new assignee is not an
active staff member) / `project_not_found`.

### DELETE /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}
Removes an assignment. Permission: `assignment.remove`. **Frozen projects
(§14.1):** removal on an archived or cancelled project returns
`400 invalid_status_transition`; on a soft-deleted project the project
resolves `404 project_not_found`.

Response `200`.

---

## Activity Feed

### GET /v1/orgs/{orgID}/activities
Lists the organization's activity feed, newest first. Permission: `activity.list`.
Paginated.

Activity items carry **`actor_name`** — the actor's display name
(`"{first_name} {last_name}"`), resolved by a users join at read time. It is
`null` (or absent) when the actor is unknown or the row predates the
enrichment; `actor_id` is retained and the UI renders the name separately
from the generic `message` (which is byte-identical to pre-enrichment copy).

**Dead-link fix (§14.3):** rows in the org feed that reference a
**soft-deleted** project render with `project_id: null` — the event and its
message stay in the feed (history preserved) but no longer link to the
deleted project. Rows referencing live projects are unaffected. The
project-scoped feed below never nulls `project_id` (it only resolves inside a
live project).

Response `200`:

```json
{
  "items": [
    {
      "id": "uuid",
      "organization_id": "uuid",
      "project_id": "uuid",
      "actor_id": "uuid",
      "actor_name": "Chiamaka Okafor",
      "milestone_id": "uuid",
      "type": "project.created",
      "message": "created project Website Redesign",
      "metadata": { "project_name": "Website Redesign" },
      "created_at": "2026-08-11T10:00:00Z"
    }
  ],
  "pagination": { "page": 1, "limit": 20, "total": 1 }
}
```

### GET /v1/orgs/{orgID}/projects/{projectID}/activities
Lists the activity feed scoped to a single project, newest first. Permission:
`activity.project.list`. Client-role actors are restricted to their own project
(else `404 project_not_found`); this is the activity surface clients may see.

Response `200`: paginated list of activity objects (shape above).

Errors: `404` `project_not_found` (client actor outside their project).

Activity `type` values include: `project.created`, `project.archived`,
`project.completed`, `project.status_changed`, `project.updated`,
`project.client_assigned`, `project.client_removed`,
`project.revision_limit_changed`, `project.deleted`, `project.restored`,
`milestone.created`, `milestone.started`, `milestone.completed`,
`milestone.cancelled`, `milestone.status_changed`, `milestone.updated`,
`milestone.submitted`, `milestone.approved`, `milestone.changes_requested`,
`milestone.payment_status_changed`, `milestone.revision_limit_changed`,
`deliverable.submitted`, `deliverable.removed`,
`client.provisioned`, `client.credential_rotated`,
`assignment.created`, `assignment.updated`, `assignment.removed`.

`project.deleted` ("Project deleted") is written by the soft-delete endpoint
and `project.restored` ("Project restored") by the archived→active restore;
both carry the acting user's `actor_name` (§14.3). A few other declared types
are reserved vocabulary but not currently emitted: `project.archived`,
`project.completed`, `project.status_changed`, `milestone.started`,
`milestone.completed` and `milestone.cancelled` — the status state machine
writes `milestone.status_changed` for every transition (including
`completed`/`cancelled`), project status changes write `project.updated`
(except the restore, which emits `project.restored`), and delete writes
`project.deleted`. The reserved types exist so the vocabulary is stable for
clients.

---

## Roles & Permissions

Memberships point at a role. Requests to organization-scoped routes are granted
when the member's role holds the required permission key. Every decision is
written to the `authz_decisions` table.

| Permission | Owner | Admin | Member | Viewer | Client |
|------------|:-----:|:-----:|:------:|:------:|:------:|
| `member.list` | x | x | | | |
| `member.invite` | x | x | | | |
| `member.role.update` | x | | | | |
| `member.remove` | x | | | | |
| `project.create` | x | x | | | |
| `project.list` | x | x | x | x | |
| `project.view` | x | x | x | x | x |
| `project.update` | x | x | | | |
| `project.status.update` | x | x | | | |
| `project.client.assign` | x | x | | | |
| `milestone.create` | x | x | | | |
| `milestone.list` | x | x | x | x | |
| `milestone.view` | x | x | x | x | x |
| `milestone.update` | x | x | | | |
| `milestone.status.update` | x | x | | | |
| `milestone.submit` | x | x | x | | |
| `milestone.approve` | x\* | | | | x |
| `milestone.revision.request` | x\* | | | | x |
| `milestone.payment_status.update` | x | x | | | |
| `deliverable.submit` | x | x | x | | |
| `assignment.create` | x | x | | | |
| `assignment.list` | x | x | x | x | |
| `assignment.view` | x | x | x | x | |
| `assignment.update` | x | x | | | |
| `assignment.remove` | x | x | | | |
| `org.view` | x | x | x | x | |
| `org.update` | x | x | | | |
| `activity.list` | x | x | x | x | |
| `activity.project.list` | x | x | x | x | x |
| `client.provision` | x | x | | | |
| `client.list` | x | x | | | |

\* `milestone.approve` and `milestone.revision.request` are seeded to the
owner role (the owner seed grants every permission in the system), but the
service layer additionally requires the actor to be the project's assigned
client (`404 project_not_found` otherwise) — in practice these actions are
client-only. The `client` role is system-seeded with only `project.view`,
`milestone.view`, `milestone.approve`, `milestone.revision.request` and
`activity.project.list`; clients are never granted list, org, member, or
assignment keys, and their access is additionally project-scoped at the
service layer.

---

## Error Codes

| HTTP | Code | Meaning |
|------|------|---------|
| 400 | `validation_error` | Request validation failed; `fields` carries details |
| 400 | `invalid_request_body` | Request body could not be decoded, or a path parameter is not a UUID |
| 400 | `invalid_organization_id` | `orgID` path parameter is not a UUID |
| 400 | `invalid_identifier` | Email/phone identifier is invalid |
| 400 | `invalid_project_priority` | Project priority is invalid |
| 400 | `invalid_status_transition` | Status change is not allowed |
| 400 | `invalid_due_date` | Due date is invalid |
| 400 | `weak_password` | Password is too weak (or identical to the current password) |
| 400 | `project_has_no_client` | Project has no client; submit-for-approval is unavailable |
| 400 | `deliverable_required` | At least one deliverable is required before submission |
| 401 | `invalid_credentials` | Failed login (unknown identifier, suspended account, or wrong password) |
| 401 | `invalid_password` | Current password is wrong |
| 401 | `unauthorized` | Missing or invalid access token |
| 401 | `invalid_token` | Refresh token invalid or expired (or malformed Authorization header) |
| 401 | `session_expired` | The access token's session row is missing, revoked, or expired |
| 401 | `session_revoked` | Session was revoked (refresh-token reuse detection) |
| 403 | `forbidden` | Authenticated but not allowed |
| 403 | `insufficient_permissions` | Reserved — mapped in the error handler, but no current route produces it (permission denials return `forbidden`) |
| 403 | `organization_suspended` | Organization is suspended |
| 403 | `password_change_required` | Must change the one-time password before proceeding |
| 404 | `organization_not_found` | Organization does not exist or is inactive |
| 404 | `membership_not_found` | No membership for this user in the org |
| 404 | `project_not_found` | Project does not exist, is outside the org, or has been soft-deleted |
| 404 | `milestone_not_found` | Milestone does not exist or is outside the org |
| 404 | `assignment_not_found` | Assignment does not exist or is outside the org |
| 404 | `client_not_found` | User is not an active client of the org |
| 404 | `deliverable_not_found` | Deliverable does not exist or is outside the milestone |
| 405 | `method_not_allowed` | Defined in the error mapper; in practice Go's `ServeMux` answers wrong-method requests with a plain-text `405` (no JSON envelope) |
| 409 | `user_not_found` | User does not exist |
| 409 | `email_already_exists` | Email is already registered |
| 409 | `phone_already_exists` | Phone is already registered |
| 409 | `organization_slug_exists` | Organization slug is already taken |
| 409 | `already_member` | User already belongs to the organization |
| 409 | `invitation_pending` | Invitation is already pending for this user |
| 409 | `milestone_not_awaiting_approval` | Milestone is not in `awaiting_approval` state |
| 409 | `client_attached_to_project` | Client membership is attached to a project; unassign instead |
| 413 | `payload_too_large` | Request body exceeds the 1MB limit |
| 429 | `rate_limited` | Reserved — mapped in the error handler, but the rate limiter returns a plain-text `429` before the JSON envelope |
| 500 | `internal_server_error` | Unexpected server error |
| 503 | `service_unavailable` | Database unavailable (readiness probe) |
