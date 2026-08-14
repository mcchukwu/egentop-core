# Egentop — Roadmap

> Statuses: **[committed]** decided and will be done · **[planned]** sequenced intent, not yet started · **[proposed]** discussed, awaiting decision · **[deferred]** explicitly shelved
> Note: `docs/roadmap.md` is now reconciled to this roadmap (2026-08-14, Q9 resolved) — a truthful human-readable summary pointing here as canonical. This file is the current product roadmap.
> Last updated: 2026-08-14

## Vision

An AI-powered operations platform for service businesses, built in four layers that grow over time:

1. **Workflow** — projects, milestones, approvals, revisions, audit history
2. **Operations** — automation, templates, notifications, reporting, team collaboration
3. **Financial** — invoicing, milestone payments, escrow, payouts, reconciliation
4. **Intelligence** — AI agents that plan, monitor, summarize, detect risks, coordinate work

Each layer unlocks the next. Escrow is a feature of Layer 3, not the product. AI (Layer 4) is not the MVP.

## Major Milestones

- [x] **M1 — Backend MVP** (complete): auth, orgs, memberships, projects, milestones, assignments, activity, audit, RBAC
- [~] **M2 — Layer-1 product delta**: approvals state machine, revisions, client role, deliverables, payment status — **backend complete + verified 2026-08-14** (93 integration tests). Remaining M2 scope: email delivery + invitation loop + password reset (wedge-independent, deferred past MVP per Q11a)
- [ ] **M3 — Frontend**: minimal client-facing portal + agency workspace — **DEFERRED 2026-08-14 (Q5: API-first validation before any frontend)**; revisit when validation signals justify the build
- [ ] **M4 — Operations layer**: templates, automation, notifications, reporting
- [ ] **M5 — Financial layer**: milestone invoicing → payment tracking → escrow/payouts
- [ ] **M6 — Intelligence layer**: AI project manager, scope analyzer, meeting summarizer, approval assistant, risk detection, ops assistant

## Near-Term Work (next ~90 days)

- [x] **Competitive research** (done 2026-08-13) — wedge validated and reframed; founder-approved 2026-08-14
- [x] **Fix tenant-isolation hole** (done 2026-08-13)
- [x] **Layer-1 delta backend** (done 2026-08-14) — client role (Option A: user + membership + `client` system role + `projects.client_id` + service-layer project scoping); milestone approval state machine; revision counter + `milestone_revisions` history + `limit_reached` at read; link-based deliverables; per-milestone payment status; client-facing approval deep link + project-scoped activity; `must_change_password` gate; credential rotation with session revocation. Verified: 93 integration tests + unit tests, migration round-trip, security review.
- [x] **AI-readiness groundwork** (done 2026-08-14, folded into the delta)
- [x] **Reliability pass** (done 2026-08-14) — security exposure review; reliability batch (`b536853`: revision_limit setter, client removal endpoint, getClientIP hardening, error-mapper 400s, login 401 unification, 1MB body limit, LOG_LEVEL wiring); **assign/remove race fixed** (`9606e7b`, Database Specialist design, Tester-verified, Reviewer-approved); **CI test gate shipped** (`817c0c1`); **validation deployment artifacts + runbook** (`817c0c1`: Dockerfile, nginx TLS proxy with sanitized XFF + edge rate limit, prod compose, systemd, env template, retention cron); docs verified against code + `docs/roadmap.md` reconciled — Q9 resolved (`f3d0230`). **134/134 tests, 0 failures.**
- [in progress] **Validation readiness** — founder picks provider/region (~$5–10/mo; London/Frankfurt recommended for WA latency); DevOps stands up the instance per `docs/deployment.md` + smoke checklist; API validation kit (wedge walkthrough, Postman collection, client deep-link demo); Product validation protocol (what to learn from 1–2 friendly agencies)
- [proposed] **Email delivery** — buy (Resend/SES/Postmark class), one provider interface. Foundation for invites, reset, verification. Recommended; awaiting approval.
- [proposed] **Close invitation loop** — accept/decline staff invitations; password reset. Client provisioning already works via one-time credentials (no email needed).
- [proposed] **Queued follow-up: deterministic membership-lock ordering** — eliminates the residual symmetric client-swap deadlock (rare, corruption-free 500). Small; not a validation blocker.
- [deferred → revisit at deploy] **CI test gate** — SHIPPED 2026-08-14 (`817c0c1`). Note: must NOT run `-race` on the whole suite (pre-existing bcrypt-cost-12 timeout); scoped `-race` packages are clean.

## Medium-Term Work

- [proposed] **Frontend** — timeboxed, hard-scoped: auth, orgs, projects, milestones, assignments, activity feed, one view; the client approval surface must be dead simple (client-facing). 8–12 weeks budgeted honestly. Decision pending (OPEN_QUESTIONS Q5: build vs agent-first validation).
- [proposed] **Templates & automation** — reusable project templates, auto-signoff, automatic reminders (Layer 2)
- [proposed] **Invoicing + per-milestone payment tracking** — agency-facing, no client trust required (Layer 3 phase 1)

## Future Work

- [proposed] **Escrow / milestone payments / automatic releases / split payments / contractor payouts / commission distribution** — Layer 3 phase 2; only after clients have interacted with the platform (trust asymmetry — see OPEN_QUESTIONS.md)
- [proposed] **AI layer** — AI project manager (proposes milestones from a brief), scope analyzer (flags out-of-scope requests), meeting summarizer, approval assistant (project history Q&A), risk detection (delayed approvals, revision blowout), operations assistant ("which projects will miss deadlines?"). Consumes the audit/activity substrate.
- [proposed] **Custom organization roles** — org-scoped roles beyond the four system templates (schema already supports `organization_id`-scoped roles)
- [proposed] **Search/filtering on list endpoints** — by status, priority, assignee

## Deferred

- [deferred] Comments / discussions on projects
- [deferred] Attachments & file uploads (S3-compatible object storage) — start with links instead
- [deferred] Real-time activity (WebSocket/SSE)
- [deferred] Time tracking and reporting
- [deferred] Notifications (in-app, email, push)
- [deferred] Email verification flow (column dormant; re-add flag + gate + flow together)
- [deferred] Phone verification flow
- [deferred] Soft-delete for projects and milestones
- [deferred] Multi-instance rate limiting (Redis) and horizontal scaling
- [deferred] OpenAPI/Swagger generation
- [deferred] Kubernetes manifests / Helm chart
- [deferred] CI/CD pipeline (lint, vet, tests, build, deploy) — deploy-time, devops agent
- [deferred] Observability: Prometheus metrics, OpenTelemetry tracing
- [deferred] Localization