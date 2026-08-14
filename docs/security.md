# Security Practices

This document describes the security controls implemented in Egentop-Core and
the operational responsibilities that remain.

## Authentication

- Passwords are hashed before storage (bcrypt). Plaintext passwords never touch
  the database.
- Access tokens are short-lived JWTs (default 15m) and carry the user ID and
  session ID as claims.
- Refresh tokens are:
  - stored server-side in the `sessions` table as a hash (never raw),
  - delivered only as an `HttpOnly`, `SameSite=Lax` cookie (inaccessible to
    JavaScript),
  - marked `Secure` when `APP_ENV=production` so they are only sent over HTTPS,
  - rotated on every refresh — each refresh invalidates the previous token.
- **Login is anti-enumeration by design**: an unknown identifier, a suspended
  account, and a wrong password all return the same `401 invalid_credentials`.
  There is no `403 forbidden` and no per-state code on login. This is an
  accepted trade-off: registration still returns its own `409
  email_already_exists` / `phone_already_exists` responses, so account
  existence is enumerable through the register endpoint by design.

## Session Management & Token Rotation

- Every login creates a session with a `token_family_id`.
- Rotating a token keeps the same family but stores a new hash.
- **Reuse detection**: presenting a revoked refresh token is treated as token
  theft and revokes the entire token family, logging the user out on all
  devices.
- `POST /v1/auth/logout` revokes the current session; `logout-all` revokes
  every session for the user.
- Changing your password revokes all other sessions.

## Client Trust Boundary

Clients are real users holding a `client`-role membership — there is no
separate clients table and no separate auth path. Their access is deliberately
narrow at three layers:

1. **RBAC** — the `client` role is seeded with only `project.view`,
   `milestone.view`, `milestone.approve`, `milestone.revision.request` and
   `activity.project.list`. Clients are never granted list, org, member, or
   assignment keys (`project.list`, `milestone.list`, `org.*`, `member.*`,
   `assignment.*`), so org-wide enumeration is impossible through the
   permission system.
2. **Project scope (service layer)** — because the permission system has no
   resource dimension, client-role reads and actions additionally resolve only
   when `projects.client_id` equals the actor's user ID. Any other project —
   including one belonging to a different client — resolves to
   `404 project_not_found` so existence never leaks.
3. **Query level** — `member.list` excludes client-role memberships, and
   clients cannot be listed or managed through the staff membership endpoints
   (see [Escalation guards](#escalation-guards) below).

The approval surface (`GET .../projects/{projectID}/approval`, milestone
detail, project-scoped activity) strips agency-facing fields (`revision_limit`,
`limit_reached`) before it reaches a client.

### One-time credential lifecycle

Clients are provisioned by the agency with a one-time credential, never by
self-registration:

- **Provisioning** (`POST /v1/orgs/{orgID}/clients`) — an existing user is
  reused without a credential (their own password stays authoritative); a new
  user receives a cryptographically random 16-character one-time password
  (~93 bits of entropy, ambiguous characters excluded). The plaintext is
  returned exactly once; only its bcrypt hash (cost 12) is stored.
- **Forced rotation** — provisioned users have `must_change_password = true`.
  The `RequirePasswordChanged` middleware returns
  `403 password_change_required` on every authenticated route except
  `POST /v1/me/password` (the cookie-authenticated `refresh` / `logout` /
  `logout-all` routes are exempt). The gate lifts only when the user changes
  the password. The one-time credential is agency-visible, so the client must
  rotate it before they can act.
- **Rotation by the agency** (`POST .../clients/{userID}/reset-credential`) —
  replaces the password hash with a fresh one-time credential, re-arms
  `must_change_password = true`, and revokes **all** of the client's sessions
  in the same transaction. A client still logged in with the old credential
  loses access immediately.

### Escalation guards

The client membership is the single representation of "this user is a client",
so it is protected against every path that could escalate or strand it:

- `member.role.update` rejects `client` as the target role and rejects any
  membership that currently holds the client role (`403 forbidden`) — a client
  can neither be escalated to a staff role nor re-role'd into one, and staff
  cannot be demoted into the client role through this endpoint (the DTO
  `oneof` validation rejects `client` outright).
- `member.remove` on a client-role membership returns
  `409 client_attached_to_project` — a client is removed exclusively through
  the project unassign flow (`PUT .../projects/{projectID}/client` with
  `null`), which prunes the membership only after the client holds no other
  project in the organization.
- Clients have no `assignment.*` keys, so assignments (staff workload) are
  never visible to or manipulable by clients.

## Authorization (RBAC)

- Permissions are enforced per request by the RBAC middleware using the
  requester's membership role in the loaded organization.
- Every authorization decision — allowed or denied — is recorded in the
  `authz_decisions` table with the permission key and a reason, providing a
  complete audit trail.
- The owner role cannot be removed from an organization, and members cannot
  delete other members' access outside their granted permissions.

## Multi-Tenant Isolation

- Tenancy is derived from the URL `{orgID}`, validated by middleware, and
  every query filters by the resolved `organization_id`.
- Services never trust foreign IDs from the client without scoping them to the
  tenant. Even if an attacker guesses a UUID in another org, the query returns
  no rows.
- `LoadOrg` rejects requests for suspended or deleted organizations before any
  business logic runs.

## Input Validation

- All DTOs are validated with `go-playground/validator/v10` before reaching the
  service layer; field-level error messages are returned to the client.
- Email and phone inputs are normalized before validation/storage.
- Raw SQL is avoided in favor of parameterized queries; user input is always
  passed as parameters, never interpolated, preventing SQL injection.
- UUID path/body values are parsed and rejected when malformed.

## Transport & Headers

- All responses carry security headers set by `SecurityHeaders` middleware:
  `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`,
  `X-XSS-Protection`, `Strict-Transport-Security`,
  `Referrer-Policy: strict-origin-when-cross-origin`, and a restrictive
  `Permissions-Policy`.
- CORS is restricted to the configured `CORS_ALLOWED_ORIGINS` allowlist.
- The refresh cookie is `Secure` in production, so it never crosses HTTP.

## Rate Limiting

In-memory per-instance rate limits protect auth and password endpoints (see the
API docs for limits). Because the limiter is per-instance in-memory, production
deployments behind a load balancer should also enforce limits at the edge for
consistent coverage.

**Keying and trust boundary.** The limiter keys on the request's real IP: the
`X-Real-IP` header when it parses as a valid IP address, otherwise the socket
peer (`RemoteAddr`). `X-Forwarded-For` is **never** used — it is
attacker-controllable and would allow both bypass (each forged value creates a
fresh bucket) and memory exhaustion (unbounded limiter-map growth). The key is
length-capped (64 bytes) so even a pathological fallback string cannot inflate
the map.

Because `X-Real-IP` is also client-supplied at the socket, the application is
**only safe behind a sanitizing proxy** (see `docs/deployment.md`): the proxy
must overwrite both `X-Real-IP` and `X-Forwarded-For` with the IP it actually
observed on the TCP connection (`$remote_addr`). A directly-exposed instance
would let a client forge `X-Real-IP: <any valid IP>` and bypass the limit.
The nginx edge rate limit is the first line of defense; the app's per-endpoint
limits are the second.

**Request-body limit.** Every request body is bounded to 1 MiB by middleware
(`MaxBytesReader`); an overrun is returned as `413 payload_too_large`. This
prevents unbounded streaming on any route, public or authenticated.

## Audit Logging

- `audit_logs` records business events (who did what, to which entity, with
  metadata) inside the same transaction as the mutation.
- `authz_decisions` records authorization attempts.
- **Scope denials carry resource identity** — when the service layer denies a
  request the permission system cannot express (a client actor outside their
  project scope), it records a denied `authz_decisions` row with
  `resource_type`/`resource_id` populated (e.g. the project), so denial
  analytics can answer per-resource questions.
- Layer-1 business events use versioned metadata
  (`{"schema_version": 1, "before", "after", "reason"}`) with stable action
  keys, and the milestone state machine writes an audit row on every
  transition — that row is the status-transition history.
- This gives both an operational trail and a security investigation trail.

## Error Handling

- Errors are mapped to generic, non-revealing messages. Internal failures
  return `internal_server_error` without leaking stack traces or SQL details.
- Client-triggerable bad input — undecodable bodies, invalid `orgID`/identifier
  path values, invalid project priorities — maps to 4xx codes
  (`invalid_request_body`, `invalid_organization_id`, `invalid_identifier`,
  `invalid_project_priority`) instead of `500`. (Unsupported HTTP methods are
  answered with a plain-text `405` by Go's `ServeMux`; the mapper also defines
  a `method_not_allowed` code.)
- Validation errors intentionally expose field names only.

## Operational Responsibilities

Deployment owners must:

- Keep `JWT_SECRET` secret, random, and long (>= 32 characters). Rotate it by
  re-issuing sessions; do not commit it to source control.
- Serve the API over HTTPS in production (`APP_ENV=production`).
- Run the latest supported PostgreSQL and apply migrations only after backup.
- Monitor `authz_decisions` and `sessions` for suspicious patterns (e.g.
  unusual denial rates, token-family revocations).
- Back up the database regularly.
