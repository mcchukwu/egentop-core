# Egentop — Product Roadmap (summary)

> **This file is a human-readable summary. The canonical, maintained roadmap
> is [`.captain/ROADMAP.md`](../.captain/ROADMAP.md)** (project memory, owned by
> the Captain). If this file and the Captain's roadmap disagree, the Captain's
> roadmap wins. This summary is updated at release/phase boundaries.
>
> Status legend: `[x]` done · `[~]` in progress · `[ ]` planned · `[deferred]` shelved.

## Product vision

Egentop is an AI-powered operations platform for service businesses (the wedge:
small agencies, Nigeria/West Africa first), built in four layers that grow over
time:

1. **Workflow** — projects, milestones, approvals, revisions, audit history
2. **Operations** — automation, templates, notifications, reporting, team collaboration
3. **Financial** — invoicing, milestone payments, escrow, payouts, reconciliation
4. **Intelligence** — AI agents that plan, monitor, summarize, detect risks, coordinate work

Each layer unlocks the next. Escrow is a Layer-3 feature, not the product. AI
(Layer 4) is not the MVP. The milestone-level sign-off → revision limits →
invoicing → payment-status wedge is the near-term product focus, validated
API-first.

## Current phase: API-first validation (no frontend)

Decided 2026-08-14 (Q5): **validate the wedge over the API before building any
frontend.** 1–2 friendly agencies use the API directly — client approval deep
link + one-time credentials shared over WhatsApp. A frontend is deferred until
validation signals justify the build.

## Done

- [x] **Backend MVP** — auth (register/login/refresh rotation/logout/logout-all,
      password change with session revocation), user profile, organizations,
      memberships (add/invite/list/role-update/remove), projects, milestones,
      assignments, activity feed, audit log, RBAC with system roles
      (owner/admin/member/viewer/client), health endpoints.
- [x] **Tenant-isolation fix** (2026-08-13) — project/milestone reads are
      org-scoped; cross-tenant reads are indistinguishable from not-found.
- [x] **Layer-1 delta** (2026-08-14) — client role + provisioning with one-time
      credentials and a `must_change_password` gate; project client assignment
      with membership pruning; milestone approval state machine
      (submit/approve/changes-requested + generic status PATCH); revision
      counting + history (`milestone_revisions`) + configurable revision
      limits with a `limit_reached` flag; link-based deliverables; per-milestone
      payment status (display-only); client-facing approval deep link +
      project-scoped activity; membership escalation guards. Every transition
      writes a versioned audit event (AI-readiness).
- [x] **Reliability batch** (2026-08-14) — `revision_limit` admin setter
      (project default + per-milestone override); provisioned-but-unassigned
      client removal endpoint; login anti-enumeration (uniform `401
      invalid_credentials`); rate-limit keying hardened (X-Real-IP/RemoteAddr,
      never X-Forwarded-For); error-mapper additions (400/405 instead of 500
      for client-triggerable input); 1 MiB body limit (`413
      payload_too_large`); `LOG_LEVEL` wired (config + logger threshold);
      dead `RATE_LIMIT_*` config removed; **client assign/remove race fix**
      (the removal serializes on the membership row lock, so a concurrent
      assignment either commits first and blocks the removal with 409, or the
      removal commits first and the assignment aborts with `404
      client_not_found` — never an orphaned project reference).
- [x] **CI test gate** — GitHub Actions runs vet/build/test against a live
      PostgreSQL 18 service container on push and pull requests (plain `go
      test ./...`; `-race` on the whole suite remains incompatible with the
      bcrypt-cost-12 integration tests).
- [x] **Verification** — integration tests against PostgreSQL + unit tests for
      pure logic (state machine, versioned audit metadata, revision-limit
      computation, OTP generator, URL validation) — **134 tests**; security
      review of the validation-exposure surface; client assign/remove race
      stress test (40 concurrent iterations, consistent outcomes only);
      migration round-trip verified.
- [x] **Validation deployment artifacts** (2026-08-14) — Dockerfile
      (multi-stage, non-root, HEALTHCHECK), production compose (Postgres
      never published), nginx TLS proxy that overwrites both headers with the
      observed peer IP + edge auth rate limit, hardened systemd unit, env
      template (app-read vars only), `authz_decisions` retention cron. The
      deployment itself is the next step (see below).
- [x] **Documentation** — API reference, deployment runbook, security
      practices, architecture, development setup, coding standards.

## In progress / next (validation phase)

- [~] **Validation deployment** — minimal single-VPS deployment of the
      validation instance (compose + host nginx, TLS, edge rate limiting,
      `authz_decisions` retention cron), then the smoke-test deploy gate.
- [ ] **API validation kit** — wedge walkthrough, Postman collection,
      client deep-link demo, and a product validation protocol (what to learn
      from 1–2 friendly agencies).
- [ ] **Founder: line up 1–2 friendly agencies** and run the API-first
      validation; feed signals into pricing, distribution, and the frontend
      decision.
- [ ] **Email delivery + invitation loop** — buy an email provider
      (Resend/SES/Postmark class), close the accept/decline invitation loop,
      add password reset. Client provisioning already works without email
      (one-time credentials). Wedge-independent; deferred past the MVP.

## Planned

- [ ] **Frontend** — timeboxed and hard-scoped once validation signals justify
      it: auth, orgs, projects, milestones, assignments, activity feed, and a
      dead-simple client approval surface.
- [ ] **Operations layer (Layer 2)** — templates, automation, notifications,
      reporting.
- [ ] **Financial layer (Layer 3)** — milestone invoicing → payment tracking →
      escrow/payouts (agency-facing first; escrow only after clients have
      interacted with the platform).
- [ ] **Intelligence layer (Layer 4)** — AI project manager, scope analyzer,
      meeting summarizer, approval assistant, risk detection, ops assistant —
      consuming the audit/activity substrate.

## Deferred

- [deferred] Email verification flow (columns dormant; re-add flag + gate +
      flow together)
- [deferred] Phone verification flow
- [deferred] Comments/discussions on projects
- [deferred] Attachments & file uploads (started with links instead)
- [deferred] Real-time activity (WebSocket/SSE)
- [deferred] Time tracking and reporting
- [deferred] Notifications (in-app, email, push)
- [deferred] Soft-delete for projects and milestones
- [deferred] Custom organization roles (schema-ready, beyond the five system roles)
- [deferred] Search/filtering on list endpoints (by status, priority, assignee)
- [deferred] Multi-instance rate limiting (Redis) and horizontal scaling
- [deferred] OpenAPI/Swagger generation
- [deferred] Kubernetes manifests / Helm chart
- [deferred] Observability: Prometheus metrics, OpenTelemetry tracing
- [deferred] Localization

See [`.captain/ROADMAP.md`](../.captain/ROADMAP.md) for the full, maintained
roadmap (milestones, near-term sequencing, and open questions).
