# Egentop — Current State

> Reflects reality, not aspirations. Update as work progresses.
> Last updated: 2026-08-14

## Completed

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

- **Approved small follow-ups (2026-08-14):** `docs/deployment.md` rollback example fix (Documenter) — **DONE 2026-08-14** (rollback example now lists 000005→000001 down migrations in reverse order; uncommitted). `revision_limit` admin setter (Builder) and provisioned-but-unassigned client removal path (Builder) — **authorized, awaiting user invocation of Builder** (handoff context re-prepared on resume).
- **Security validation-exposure review DONE (2026-08-14)** — analysis only, no code changed. No Critical/multi-tenant breach; tenant isolation, client project-scoping (404 no-existence-leak), one-time-credential gate, refresh rotation/family revocation all verified sound. Findings: **HIGH-1** rate limiter trusts `X-Forwarded-For` verbatim (bypass + memory DoS; fix = proxy overwrites/strips XFF + cheap code change to derive key from `X-Real-IP`/`RemoteAddr`, cap length); **MEDIUM-1** missing error-mapper cases return 500 for client-triggerable bad input; **MEDIUM-2** login/register account enumeration (login 409 user_not_found vs 401 invalid_password); **MEDIUM-3** no request-body size limit; **MEDIUM-4** org-existence oracle 403-vs-404 (accept for validation, revisit pre-launch); LOWs: HSTS includeSubDomains, dead `RATE_LIMIT_REQUESTS`/`RATE_LIMIT_WINDOW` config, unpopulated session IP/UA, pagination overflow. Exposure checklist (proxy+TLS, XFF sanitize, APP_ENV/cookies, JWT secret, CORS, DB lockdown, authz_decisions cron, smoke test) is the deploy gate. Sample nginx config in `docs/deployment.md` uses `$proxy_add_x_forwarded_for` — insufficient; must be fixed at deploy.
- **Up next (reliability pass):** close test-coverage gaps (HTTP cross-org GET/PATCH, live concurrency-lock test); apply the cheap security fixes (HIGH-1 code half + MEDIUM-1/2/3 + LOW-2 config cleanup — recommended as a Builder batch); CI test gate decision pending founder re-review.

## Partially Implemented

- **Invitation flow** — invite-by-email creates a membership with status `invited`, but there is **no accept/decline flow** and **no email delivery**. (Client provisioning does NOT use the invite flow — it provisions directly with a one-time credential.)
- **`email_verified` / `phone_verified`** columns exist in schema, are returned in profiles, but have no flow to set them true (dormant by design)
- **`revision_limit`** — schema + read-side (effective limit = COALESCE(milestone, project)) are fully wired, but **no API endpoint sets it** (admin write endpoint is a natural follow-up; default is unlimited)

## Known Problems

- **`docs/deployment.md` rollback example — FIXED 2026-08-14** — now lists all five down migrations in reverse order (000005→000001); uncommitted in working tree.
- **`docs/roadmap.md` is legacy** — reflects the old generic-PM framing; conflicts with the four-layer vision; needs reconciliation (OPEN_QUESTIONS Q9); still contains a false "done" claim (milestone status update — the delta resolved the status path, but the file's framing is still stale).
- **`project.status.update` permission is seeded but unused** — status changes flow through `PATCH /projects/{id}` with `project.update`; permission-to-route mismatch remains (pre-existing, minor).
- **Provisioned-but-never-assigned clients cannot be removed** — `member.remove` rejects all client-role memberships (409), and unassign requires a project link; a client with no project has no API removal path. Operational note: such memberships persist in `client.list` until DB cleanup. (Tester-flagged 2026-08-14.)
- **Client `milestone.list` on a non-owned project returns 403, not 404** — clients lack the `milestone.list` permission entirely (RBAC denies before the service scope check); no existence leak; milestone *detail* correctly 404s. Accepted deviation (client key set stays narrow by design).
- **`member.role.update` targeting `client` returns 400 `validation_error`** (DTO `oneof` rejects it) rather than the documented 403 — rejection is effective; only the status code differs.
- **No CI** — all integration tests require live PostgreSQL; nothing runs them automatically (deferred to deploy time by founder decision; see DECISIONS.md). A CI gate will hit the bcrypt-cost-12 `-race` timeout if it runs `-race` on the whole suite.
- **`authz_decisions` grows unbounded** — cleanup SQL exists in the Makefile (`authz-decisions-cleanup`, 90-day window) but nothing runs it automatically; needs a cron at deploy time.
- **Rate-limit bypass risk (pre-existing)** — `getClientIP` trusts `X-Forwarded-For` unconditionally; limits bypassable if exposed without a sanitizing proxy.
- **Boundary-hardening coverage gaps (pre-existing, minor)** — dedicated HTTP cross-org project GET/PATCH tests and the live concurrency-lock test remain useful additions; service/repository-level coverage exists.

## Technical Debt

The structural-debt statement (2026-08-13 audit) is now fully resolved: the tenant-isolation hole, the dead milestone-status code, the activity/audit metadata defects, and the unit-test gap are all fixed. Remaining debt: no CI/test gate, docs drift (deployment rollback example, legacy roadmap), `authz_decisions` retention automation, full-package `-race` incompatibility with bcrypt-cost-12 tests, and the minor items above. No architectural over-engineering debt identified.

## Current Blockers

- **None technical.** Product/business decisions are the next gates: distribution (Q3), pricing (Q4), founder availability (Q6), frontend decision (Q5). The backend is ahead of the business validation loop.

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
5. **Small follow-ups (IN PROGRESS)** — `revision_limit` admin setter; provisioned-but-unassigned client removal path; `docs/deployment.md` rollback example fix
6. **Reliability pass** — close test-coverage gaps (HTTP cross-org GET/PATCH, live concurrency-lock test); security hardening for validation exposure (rate-limit/X-Forwarded-For when exposed); CI test gate decision (recommended: ship test gate before validation)
7. **Validation readiness** — minimal deployment for the validation instance (DevOps; founder picks provider/budget); API validation kit (wedge walkthrough, Postman collection, client deep-link demo); Product validation protocol (what to learn from 1–2 friendly agencies)
8. **Founder: line up 1–2 friendly agencies** and run API-first validation; feed signals into pricing (Q4), distribution (Q3), and the frontend decision
9. **Email delivery** (buy: Resend/SES/Postmark class) + invitation loop + password reset — wedge-independent, deferred past MVP
