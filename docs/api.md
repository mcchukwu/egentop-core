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
- `GET /v1/me`, `GET /v1/orgs` and `POST /v1/orgs` require only a valid access
  token. All other `/v1/orgs/{orgID}/...` routes additionally require an active
  membership in that organization and a role holding the required permission
  (see [Roles & Permissions](#roles-and-permissions)).

### Rate Limiting

| Endpoint | Limit |
|----------|-------|
| `POST /v1/auth/register` | 3 / minute |
| `POST /v1/auth/login` | 5 / minute |
| `POST /v1/auth/refresh` | 10 / minute |
| `POST /v1/me/password` | 5 / minute |
| Everything else | 100 / minute |

Exceeding a limit returns `429 Too Many Requests` with code `rate_limited`.

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
Creates a new user account. Sets the `refresh_token` cookie and returns an
access token.

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

Errors: `400` `validation_error`, `409` `email_already_exists` / `phone_already_exists`.

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

Errors: `400` `validation_error`, `401` `invalid_credentials`, `403` `forbidden`.

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
Revokes the current session and clears the `refresh_token` cookie.

Response `204`.

Errors: `401`.

### POST /v1/auth/logout-all
Revokes every session for the authenticated user and clears the `refresh_token`
cookie.

Response `204`.

Errors: `401`.

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

Errors: `400` `validation_error`, `401` `invalid_password`, `403` `email_not_verified`.

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

Errors: `400` `validation_error`, `404` `user_not_found`, `409` `already_member`.

### POST /v1/orgs/{orgID}/members/invite
Invites a user by email. The invitation is created with status `invited` if the
user exists; otherwise it is still recorded as pending. Permission:
`member.invite`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `email` | string | required, valid email |
| `role` | string | required, one of `owner`, `admin`, `member`, `viewer` |

Response `201`.

Errors: `400` `validation_error`, `409` `already_member` / `invitation_pending`.

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
| `due_date` | string (RFC 3339) | optional |

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
    "created_at": "2026-08-11T10:00:00Z",
    "updated_at": "2026-08-11T10:00:00Z"
  }
}
```

`status` is one of `draft`, `active`, `completed`, `archived`, `cancelled`.
`priority` is one of `low`, `medium`, `high`.

### GET /v1/orgs/{orgID}/projects
Lists projects in the organization. Permission: `project.list`. Paginated.

Response `200`: paginated list of project objects (shape above).

### GET /v1/orgs/{orgID}/projects/{projectID}
Returns a single project. Permission: `project.view`.

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
| `due_date` | string (RFC 3339) | optional |

Response `200` with the updated project.

Errors: `400` `validation_error` / `invalid_status_transition`, `404` `project_not_found`.

---

## Milestones

### POST /v1/orgs/{orgID}/projects/{projectID}/milestones
Creates a milestone. Permission: `milestone.create`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `title` | string | required, min 3, max 120 |
| `description` | string | optional, max 2000 |
| `due_date` | string (RFC 3339) | optional |

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
    "created_at": "2026-08-11T10:00:00Z",
    "updated_at": "2026-08-11T10:00:00Z"
  }
}
```

`status` is one of `pending`, `in_progress`, `awaiting_approval`, `completed`,
`blocked`, `cancelled`.

### GET /v1/orgs/{orgID}/projects/{projectID}/milestones
Lists milestones for a project. Permission: `milestone.list`. Paginated.

### GET /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}
Returns a single milestone. Permission: `milestone.view`.

Errors: `404` `milestone_not_found`.

### PATCH /v1/orgs/{orgID}/projects/{projectID}/milestones/{milestoneID}
Updates milestone metadata and/or status. Permission: `milestone.update`.

Request body (all fields optional):

| Field | Type | Rules |
|-------|------|-------|
| `title` | string | min 3, max 120 |
| `description` | string | max 2000 |
| `due_date` | string (RFC 3339) | optional |
| `position` | integer | optional |

Response `200` with the updated milestone.

Errors: `400` `validation_error`, `404` `milestone_not_found`.

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

### GET /v1/orgs/{orgID}/projects/{projectID}/assignments
Lists assignments for a project. Permission: `assignment.list`. Paginated.

### GET /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}
Returns a single assignment. Permission: `assignment.view`.

Errors: `404` `assignment_not_found`.

### PATCH /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}
Reassigns an assignment to a different user. Permission: `assignment.update`.

Request body:

| Field | Type | Rules |
|-------|------|-------|
| `assigned_to` | string | required, UUID |

Response `200` with the updated assignment.

### DELETE /v1/orgs/{orgID}/projects/{projectID}/assignments/{assignmentID}
Removes an assignment. Permission: `assignment.remove`.

Response `200`.

---

## Activity Feed

### GET /v1/orgs/{orgID}/activities
Lists the organization's activity feed, newest first. Permission: `activity.list`.
Paginated.

Response `200`:

```json
{
  "items": [
    {
      "id": "uuid",
      "organization_id": "uuid",
      "project_id": "uuid",
      "actor_id": "uuid",
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

Activity `type` values include: `project.created`, `project.archived`,
`project.completed`, `project.status_changed`, `project.updated`,
`milestone.created`, `milestone.started`, `milestone.completed`,
`milestone.cancelled`, `milestone.status_changed`, `milestone.updated`,
`assignment.created`, `assignment.updated`, `assignment.removed`.

---

## Roles & Permissions

Memberships point at a role. Requests to organization-scoped routes are granted
when the member's role holds the required permission key. Every decision is
written to the `authz_decisions` table.

| Permission | Owner | Admin | Member | Viewer |
|------------|:-----:|:-----:|:------:|:------:|
| `member.list` | x | x | | |
| `member.invite` | x | x | | |
| `member.role.update` | x | | | |
| `member.remove` | x | | | |
| `project.create` | x | x | | |
| `project.list` | x | x | x | x |
| `project.view` | x | x | x | x |
| `project.update` | x | x | | |
| `project.status.update` | x | x | | |
| `milestone.create` | x | x | | |
| `milestone.list` | x | x | x | x |
| `milestone.view` | x | x | x | x |
| `milestone.update` | x | x | | |
| `milestone.status.update` | x | x | | |
| `assignment.create` | x | x | | |
| `assignment.list` | x | x | x | x |
| `assignment.view` | x | x | x | x |
| `assignment.update` | x | x | | |
| `assignment.remove` | x | x | | |
| `org.view` | x | x | x | x |
| `org.update` | x | x | | |
| `activity.list` | x | x | x | x |

---

## Error Codes

| HTTP | Code | Meaning |
|------|------|---------|
| 400 | `validation_error` | Request validation failed; `fields` carries details |
| 400 | `invalid_status_transition` | Status change is not allowed |
| 400 | `invalid_due_date` | Due date is invalid |
| 400 | `weak_password` | Password is too weak |
| 401 | `invalid_credentials` | Email/phone or password is wrong |
| 401 | `invalid_password` | Current password is wrong |
| 401 | `unauthorized` | Missing or invalid access token |
| 401 | `invalid_token` | Refresh token invalid or expired |
| 401 | `session_expired` | Session has expired |
| 401 | `session_revoked` | Session was revoked |
| 403 | `forbidden` | Authenticated but not allowed |
| 403 | `insufficient_permissions` | Role lacks the required permission |
| 403 | `email_not_verified` | Email verification required first |
| 403 | `organization_suspended` | Organization is suspended |
| 404 | `organization_not_found` | Organization does not exist or is inactive |
| 404 | `membership_not_found` | No membership for this user in the org |
| 404 | `project_not_found` | Project does not exist or is outside the org |
| 404 | `milestone_not_found` | Milestone does not exist or is outside the org |
| 404 | `assignment_not_found` | Assignment does not exist or is outside the org |
| 409 | `user_not_found` | User does not exist |
| 409 | `email_already_exists` | Email is already registered |
| 409 | `phone_already_exists` | Phone is already registered |
| 409 | `organization_slug_exists` | Organization slug is already taken |
| 409 | `already_member` | User already belongs to the organization |
| 409 | `invitation_pending` | Invitation is already pending for this user |
| 429 | `rate_limited` | Too many requests |
| 500 | `internal_server_error` | Unexpected server error |
| 503 | `service_unavailable` | Database unavailable (readiness probe) |
