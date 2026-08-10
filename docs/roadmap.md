# Roadmap

Status legend: `[ ]` planned, `[~]` in progress, `[x]` done.

## MVP (current)

- [x] Authentication: register, login, refresh, logout, logout-all
- [x] Password change with session revocation
- [x] User profile (view / update)
- [x] Organizations: create, list, view, update
- [x] Memberships: add, invite by email, list, role update, remove
- [x] Projects: create, list, view, metadata + status update
- [x] Milestones: create, list, view, metadata + status update
- [x] Assignments: create, list, view, reassign, remove
- [x] Activity feed per organization
- [x] RBAC with system roles (owner/admin/member/viewer) and permission keys
- [x] Audit log and authz-decision recording
- [x] Session persistence and refresh-token rotation with reuse detection
- [x] Integration tests against PostgreSQL
- [x] Documentation: README, API reference, architecture, setup, deployment,
      security, coding standards

## Next

- [ ] Email verification flow (send + verify OTP/link)
- [ ] Phone verification flow
- [ ] Password reset (forgot password)
- [ ] Accept/decline organization invitations
- [ ] Project archiving and status transitions with explicit state machines
- [ ] Milestone status transitions with completion timestamps
- [ ] Filtering/search on list endpoints (by status, priority, assignee)
- [ ] Soft-delete for projects and milestones
- [ ] Invitation email delivery (SMTP provider integration)
- [ ] Custom organization roles (org-scoped roles beyond the four system roles)
- [ ] User settings and notifications

## Later

- [ ] Project comments / discussions
- [ ] Attachments and file uploads (S3-compatible)
- [ ] Real-time activity (WebSocket/SSE)
- [ ] Time tracking and reporting
- [ ] Notifications (in-app, email, push)
- [ ] Multi-instance rate limiting (Redis) and horizontal scaling
- [ ] OpenAPI/Swagger generation from handlers
- [ ] Kubernetes manifests / Helm chart
- [ ] CI/CD pipeline (lint, vet, tests, build, deploy)
- [ ] Observability: metrics (Prometheus), tracing (OpenTelemetry)
- [ ] Localization
