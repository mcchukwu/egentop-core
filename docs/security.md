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

## Session Management & Token Rotation

- Every login creates a session with a `token_family_id`.
- Rotating a token keeps the same family but stores a new hash.
- **Reuse detection**: presenting a revoked refresh token is treated as token
  theft and revokes the entire token family, logging the user out on all
  devices.
- `POST /v1/auth/logout` revokes the current session; `logout-all` revokes
  every session for the user.
- Changing your password revokes all other sessions.

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

In-memory rate limits protect auth and password endpoints (see the API docs for
limits). Note: because the limiter is per-instance in-memory, production
deployments behind a load balancer should also enforce limits at the edge for
consistent coverage.

## Audit Logging

- `audit_logs` records business events (who did what, to which entity, with
  metadata) inside the same transaction as the mutation.
- `authz_decisions` records authorization attempts.
- This gives both an operational trail and a security investigation trail.

## Error Handling

- Errors are mapped to generic, non-revealing messages. Internal failures
  return `internal_server_error` without leaking stack traces or SQL details.
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
