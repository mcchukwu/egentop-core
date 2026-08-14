# Egentop — Current State

> Reflects reality, not aspirations. Update as work progresses.
> Last updated: 2026-08-14

## Completed

- **RELIABILITY PASS — DONE 2026-08-14 (the backend is validation-ready):**
  - **Security validation-exposure review** — no Critical/multi-tenant breach; tenant isolation, client project-scoping (404 no-existence-leak), one-time-credential gate, refresh rotation/family revocation verified sound. Findings (all handled): HIGH-1 XFF rate-limit trust (deployment half fixed in nginx config + code half hardened), MEDIUM-1 error-mapper 500s (mapped to 4xx), MEDIUM-2 login enumeration (unified 401), MEDIUM-3 no body limit (1MB + 413), MEDIUM-4 org-existence oracle (accepted for validation), LOWs (HSTS, dead config, session IP/UA, pagination).
  - **Reliability batch (`b536853`)** — `revision_limit` admin setter (project + milestone PATCH endpoints, versioned audit, client surfaces unchanged); client removal endpoint (`DELETE /orgs/{orgID}/clients/{userID}`, 404/409 semantics, user row preserved); `getClientIP` hardened (X-Real-IP/RemoteAddr, never XFF, length cap); error mapper additions; login unified to 401 `invalid_credentials` (anti-enumeration, register 409s kept); 1MB `MaxBytesReader` middleware → 413; `LOG_LEVEL` wired (threshold filtering); dead `RATE_LIMIT_*` removed. 16 new tests.
  - **Assign/remove race FIXED (`9606e7b`)** — Tester found a MAJOR race (concurrent assign + client removal could both commit, orphaning `projects.client_id` to a deleted membership); Database Specialist designed the fix (serialize both operations on the client membership row: `IsActiveClientMember` → `FOR SHARE OF m` with tx-enforced signature; prune acquires the displaced client's membership `FOR UPDATE` first — closing the same-class prune-path bug). Tester restructured the deterministic repro to pin removal-wins→assign-404 and added prune-race deterministic + stress coverage. Reviewer APPROVED. **134/134 tests, 0 failures.**
  - **CI test gate (`817c0c1`)** — GitHub Actions `ci` on push+PR: vet/build/full suite against postgres:18 service container; plain `go test ./...` (no full-suite `-race` — bcrypt-12 constraint documented); `make test-integration` local target. CI decision: **shipped now** (Captain, on founder delegation).
  - **Validation deployment artifacts (`817c0c1`)** — Dockerfile (multi-stage, non-root, 12.9MB), `deploy/nginx` TLS proxy with sanitized X-Forwarded-For + edge auth rate limit, prod compose (Postgres not public), systemd unit, env template (only real vars), `authz_decisions` weekly cleanup cron, `docs/deployment.md` rewritten as runbook + smoke checklist. Verified: docker build+run healthy, nginx -t, full suite.
  - **Docs pass + Q9 resolved (`f3d0230`)** — api/deployment/security docs verified against code; `docs/roadmap.md` reconciled to the four-layer vision (`.captain/ROADMAP.md` canonical); README framing corrected; `.env.example` truthful (dead `APP_NAME` removed, `DB_SSLMODE` added).
- **Layer-1 delta — BUILT, TESTED, REVIEWED, SECURITY-REVIEWED (2026-08-14)** — the wedge product increment is complete on the backend:
  - **Client role + provisioning** (`internal/client`): `POST /orgs/{orgID}/clients` (provision with one-time credential; existing-user reuse; 409 already_member), `GET /orgs/{orgID}/clients` (client list), `POST .../clients/{userID}/reset-credential` (rotate credential + revoke all sessions). `users.must_change_password` gate: 403 `password_change_required` on every authenticated route except `/v1/me/password` (cookie routes exempt).
  - **Project client assignment**: `PUT .../projects/{projectID}/client` (`{client_id|null}`) — assign/reassign/unassign; unassign/reassign prune the client membership iff the client holds no other project in the org (audited).
  - **Client scope enforcement**: client-role actors resolve only their own project (`projects.client_id == actor`) — everything else 404 `project_not_found` (no existence leak), denials recorded in `authz_decisions` with resource identity.
  - **Milestone approval state machine**: `POST .../submit` (staff; idempotent; creates revision row + increments `revision_count`), `POST .../approve` (client-only; idempotent), `POST .../changes-requested` (client-only; 409 on stale state), `PATCH .../status` (staff; generic transitions; blocked on archived/cancelled projects; `approved→completed` stamps `completed_at`). `awaiting_approval→completed` is forbidden (the wedge). Cancel reachable from every non-terminal state.
  - **Revisions**: `revision_count` (submission rounds) + `milestone_revisions` history table + `revision_limit` (per-milestone override / project default, NULL = unlimited) + `limit_reached` computed at read; hidden from client surfaces.
  - **Deliverables**: `milestone_deliverables` (link-based), POST/DELETE, http(s) URLs, frozen in completed/cancelled, embedded in milestone detail + approval view.
  - **Payment status**: per-milestone `unpaid`/`partial`/`paid` (display-only, no money movement), agency-updated, audited, read-only for clients.
  - **Client-facing views**: `GET .../approval` (deep-link payload: project + milestones + deliverables + payment status) + `GET .../projects/{projectID}/activities` (project-scoped activity).
  - **Membership guards**: `member.list` excludes client-role memberships; `member.role.update` rejects client role (current or target); `member.remove` on client membership → 409 `client_attached_to_project`; assignment excludes client-role users.
  - **AI-readiness fixes landed**: `activity.NewActivity` metadata drop fixed; versioned audit metadata convention (`{"schema_version":1,"before","after","reason"}`) on all new events; `audit_logs(entity_type, entity_id, created_at DESC)` index; audit row per state transition (that row is the status-transition history); `milestone_revisions` as revision history; authz-decisions refactored into `internal/audit` with resource identity on scope denials.
  - **Migration 000005** (up/down): milestone_status column-rewrite (added `approved`, `changes_requested`; reversible in both directions, no `ALTER TYPE ADD VALUE`), payment/revision columns, `projects.client_id`, `users.must_change_password`, two history tables, audit index, 9 permission keys, `client` system role (5 narrow grants), owner-gap fix (owner now truly holds every permission), admin gap fixes (`activity.list`, `milestone.submit`, `deliverable.submit`). Down is byte-exact + data-preserving.
  - **Verification**: 93 integration tests passing (0 skip) + unit tests for pure logic (state-machine table, VersionedMetadata, revisionLimitReached, OTP generator); race-scoped runs pass on all Layer-1 logic; migration round-trip verified; live HTTP verification of the security-critical paths (session revocation on credential rotation, escalation guard, scope 404s). Tester VERIFIED all 10 post-review fixes; Reviewer: sound, changes-requested resolved; Security: no critical/high confidentiality or integrity vulnerabilities, the one HIGH (credential rotation) fixed. Full-package `-race` remains blocked by a pre-existing bcrypt-cost-12/timeout interaction (not introduced by the delta).
- **Tenant-isolation hole FIXED (2026-08-13)** — project/milestone read paths are org-scoped; unscoped repo methods deleted; regression test proves cross-tenant reads are indistinguishable from not-found.
- **Boundary hardening implemented (2026-08-13)** — project updates org-scoped + `FOR UPDATE`; nested milestone/assignment list boundaries validated; reviewer-approved; live PostgreSQL verification passed including focused race run.
- **Backend MVP complete** — auth (register email/phone, login, refresh rotation, logout, logout-all, password change with session revocation), user profile, organizations, memberships, projects, milestones, assignments, activity feed, audit log, RBAC, health endpoints.
- **Auth hardening** — refresh rotation, token families, reuse detection → family revocation, bcrypt cost 12, lookup-hash + bcrypt defense in depth, row locks, race-safe idempotent logout, HMAC-only JWT, per-request session validation, per-endpoint rate limits.
- **RBAC** — data-driven roles, single-query permission checks, every decision audited.
- **Competitive research** (2026-08-13) — wedge validated and reframed; key facts in PROJECT.md.
- **Independent architectural audit** (2026-08-13) — found the tenant-isolation hole + AI-readiness defects; all findings resolved by 2026-08-14.
- **Project memory** — `.captain/` knowledge base maintained through the Layer-1 cycle.

## Currently Being Worked On

- **Validation readiness (NEXT)** — deployment provider/region choice (founder; ~$5–10/mo; London/Frankfurt recommended for WA latency); then DevOps stands up the validation instance per `docs/deployment.md` + smoke checklist (TLS, sanitized proxy, Secure cookies, fresh JWT secret, CORS, DB lockdown, retention cron). API validation kit (wedge walkthrough, Postman collection, client deep-link demo) + Product validation protocol (what to learn from 1–2 friendly agencies) not yet built.
- **Founder:** line up 1–2 friendly agencies for the API-first validation run.
- Not in progress: email delivery + invitation loop + password reset (deferred past MVP); frontend (deferred per Q5).

## Partially Implemented

- **Invitation flow** — invite-by-email creates a membership with status `invited`, but there is **no accept/decline flow** and **no email delivery**. (Client provisioning does NOT use the invite flow — it provisions directly with a one-time credential.)
- **`email_verified` / `phone_verified`** columns exist in schema, are returned in profiles, but have no flow to set them true (dormant by design)
- **`revision_limit`** — schema + read-side + **write-side are fully wired (2026-08-14)**; effective limit = COALESCE(milestone, project), NULL = unlimited
- **`authz_decisions` cleanup** — SQL + cron artifacts exist (`deploy/cron/`), but the cron is only installed at deploy time

## Known Problems

- **Residual deadlock on symmetric concurrent client-swap reassigns (documented, queued)** — two `AssignClient` txs swapping the same two clients between two projects can deadlock (PG `40P01` → one 500, safe abort, NO corruption). Reviewer-verified; fix (acquire the two membership locks in deterministic order) is a queued follow-up, not a validation blocker.
- **Test-suite fixture leaks (housekeeping)** — pre-existing stress/provision tests lack `t.Cleanup` and leak rows into a reused test DB; new tests clean up. (Tester-flagged 2026-08-14.)
- **`{}` body clears `revision_limit`** — the setter treats absent/null as "clear to unlimited" (documented, consistent with the `AssignClientRequest` precedent); an empty JSON object silently unlimits. Acceptable for the internal admin surface.
- **Stale comment in `deploy/nginx/egentop.conf`** — says "the app has no body-size limit of its own"; the app now enforces 1MB (b536853). Config behavior correct; fix the comment at deploy time (DevOps).
- **`project.status.update` permission is seeded but unused** — status changes flow through `PATCH /projects/{id}` with `project.update`; permission-to-route mismatch remains (pre-existing, minor).
- **Client `milestone.list` on a non-owned project returns 403, not 404** — clients lack the `milestone.list` permission entirely (RBAC denies before the service scope check); no existence leak; milestone *detail* correctly 404s. Accepted deviation (client key set stays narrow by design).
- **`member.role.update` targeting `client` returns 400 `validation_error`** (DTO `oneof` rejects it) rather than the documented 403 — rejection is effective; only the status code differs.
- **`authz_decisions` grows unbounded** — cleanup SQL + cron artifacts exist (`deploy/cron/`, Makefile) but the cron is installed only at deploy time.
- **MEDIUM-4 org-existence oracle (accepted for validation)** — valid-but-unowned `{orgID}` → 403 vs nonexistent → 404; revisit before broader launch.
- **Full-package `-race` incompatible with bcrypt-cost-12 tests** — CI and local runs use plain `go test ./...`; scoped `-race` on non-bcrypt packages is clean.

## Technical Debt

The structural-debt statement (2026-08-13 audit) is fully resolved, and the reliability-pass debt is now also resolved: tenant-isolation hole, dead milestone-status code, activity/audit metadata defects, unit-test gap, XFF rate-limit trust, error-mapper gaps, login enumeration, missing body limit, dead config, assign/remove race (all fixed 2026-08-14). Remaining: full-package `-race` incompatibility with bcrypt-cost-12 tests (documented), `authz_decisions` retention cron install (deploy-time), the residual swap deadlock (queued follow-up, documented), test-fixture-leak housekeeping, and the minor items above. No architectural over-engineering debt identified.

## Current Blockers

- **None technical.** 134/134 tests, CI-gated, race-free, review-approved. The next gates are founder decisions: validation provider/region choice, agency recruitment. The backend is ahead of the business validation loop.

## Current Assumptions

- Agencies' willingness to pay rests on workflow clarity improving cash flow — **unvalidated** (the wedge is approved but untested with real agencies)
- Target geography Nigeria/West Africa — **confirmed by founder 2026-08-14**
- Link-based deliverables suffice initially (no storage infra needed) — **implemented; unvalidated with real clients**
- WhatsApp as the client channel (one-time credentials shared out-of-band) — **implemented; unvalidated**

## What Should Happen Next

1. ~~**Founder decisions** — geography (Q1), wedge (Q2)~~ — **DONE 2026-08-14**
2. ~~Fix tenant-isolation hole~~ — **DONE 2026-08-13**
3. ~~**Layer-1 delta** — product requirements → architecture → migration → plan → build → test + review + security~~ — **DONE 2026-08-14** (backend complete; 93 integration tests; all verifier findings resolved)
4. ~~**Q5/Q6 decisions** — API-first validation before frontend; founder available daily~~ — **DONE 2026-08-14**
5. ~~**Small follow-ups + reliability pass** — revision_limit setter, client removal, security hardening, race fix, CI gate, deployment artifacts~~ — **DONE 2026-08-14** (commits `b536853`/`9606e7b`/`817c0c1`/`f3d0230`; 134/134 tests; Reviewer APPROVED)
6. **Validation deployment (NEXT)** — founder picks provider/region (~$5–10/mo; London/Frankfurt recommended) → DevOps stands up the instance per `docs/deployment.md` runbook + smoke checklist (TLS, sanitized proxy, Secure cookies, fresh JWT secret, CORS, DB lockdown, authz_decisions cron)
7. **Validation kit + protocol** — API validation kit (wedge walkthrough, Postman collection, client deep-link demo) + Product validation protocol (what to learn from 1–2 friendly agencies)
8. **Founder: line up 1–2 friendly agencies** and run API-first validation; feed signals into pricing (Q4), distribution (Q3), and the frontend decision
9. **Email delivery** (buy: Resend/SES/Postmark class) + invitation loop + password reset — wedge-independent, deferred past MVP
