# Egentop — Roadmap

> Statuses: **[committed]** decided and will be done · **[planned]** sequenced intent, not yet started · **[proposed]** discussed, awaiting decision · **[deferred]** explicitly shelved
> Note: `docs/roadmap.md` is the legacy MVP-era roadmap (generic PM framing) and predates the four-layer vision — it needs reconciliation. This file is the current product roadmap.
> Last updated: 2026-08-13

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
- [x] **Layer-1 delta backend** (done 2026-08-14) — client role (Option A: user + membership + `client` system role + `projects.client_id` + service-layer project scoping); milestone approval state machine (reuses `awaiting_approval`; adds `approved` + `changes_requested`; submit/approve/changes-requested action endpoints + generic status PATCH); revision counter + `milestone_revisions` history + `limit_reached` at read; link-based deliverables; per-milestone payment status; client-facing approval deep link + project-scoped activity; `must_change_password` gate; credential rotation with session revocation. Every transition writes a versioned audit event (AI-readiness). Verified: 93 integration tests, race-scoped clean, migration round-trip, security review.
- [x] **AI-readiness groundwork** (done 2026-08-14, folded into the delta) — activity metadata bug fixed; versioned audit metadata convention; `audit_logs(entity_type, entity_id, created_at)` index; audit row per state transition; `milestone_revisions` as revision history
- [x] **Unit tests for pure logic** (done 2026-08-14) — state-machine transition table, VersionedMetadata, revisionLimitReached, OTP generator, URL validation
- [proposed] **Email delivery** — buy (Resend/SES/Postmark class), one provider interface. Foundation for invites, reset, verification. Recommended; awaiting approval.
- [proposed] **Close invitation loop** — accept/decline staff invitations; password reset. Client provisioning already works via one-time credentials (no email needed).
- [proposed] **`revision_limit` admin setter** — schema + read-side wired; only the write endpoint is missing (small).
- [x] **Validation-path decision** (done 2026-08-14) — API-first validation before any frontend (Q5); founder available daily (Q6)
- [in progress] **Small engineering follow-ups** — `revision_limit` admin setter, provisioned-but-unassigned client removal path, `docs/deployment.md` rollback example fix
- [proposed] **Reliability pass** — close test-coverage gaps (HTTP cross-org GET/PATCH, live concurrency-lock test); security hardening for validation exposure; CI test gate (recommended: ship before validation, pending founder re-review of the deploy-time deferral)
- [proposed] **Validation readiness** — minimal deployment for the validation instance (DevOps); API validation kit (wedge walkthrough, Postman collection, client deep-link demo); Product validation protocol (what to learn from 1–2 friendly agencies)
- [deferred → revisit at deploy] **Minimal CI test gate** — GitHub Actions + Postgres service container; founder deferred CI to deploy time; recommendation stands. Note: a CI gate must NOT run `-race` on the whole suite (pre-existing bcrypt-cost-12 timeout); scope `-race` to specific packages or lower bcrypt cost in tests.

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