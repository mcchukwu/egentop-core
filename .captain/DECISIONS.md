# Egentop — Decision Record

> Append-only. Never rewrite history: if a decision changes, add a new entry explaining what changed and why.
> Latest first.

---

## 2026-08-14 — Reliability pass complete; race fix accepted; validation deployment next

**Decision:** The reliability pass is complete and the backend is validation-ready: **134/134 tests pass, 0 failures**, CI gate live, Reviewer APPROVED. Decisions made within this pass:

1. **CI test gate shipped now** (`817c0c1`) — on founder delegation, the Captain decided the pre-deploy deferral is re-opened: ship the test gate before validation (not at deploy). GitHub Actions `ci` on push+PR with postgres:18 service container; plain `go test ./...` (never full-suite `-race` — bcrypt-cost-12 timeout, documented).
2. **Security findings applied** (`b536853`) — HIGH-1 XFF trust (code half + nginx sanitization at deploy), MEDIUM-1/2/3, LOW-2; MEDIUM-4 (org-existence oracle) **accepted for validation**, revisit pre-launch.
3. **`LOG_LEVEL` wired, not removed** — it was a non-functional config var; the project's "config surface must be truthful" principle plus the ops checklist favored implementing threshold filtering (small) over deleting the var.
4. **Assign/remove race fix accepted** (`9606e7b`) — Tester found a MAJOR race (concurrent assign + client removal could both commit, orphaning `projects.client_id` to a deleted membership; reproduced 5/5). The Database Specialist designed the fix: serialize assign and remove on the client membership row (`IsActiveClientMember` → `FOR SHARE OF m`, tx-enforced signature; prune acquires the displaced client's membership `FOR UPDATE` first — closing the same-class prune-path orphan). The Tester's naive candidates were rejected on the merits: locking `ClientHasProjects` is insufficient (locks only already-existing references) and would create an AB-BA deadlock cycle. Reviewer: APPROVED.
5. **Residual swap deadlock deferred (documented, queued)** — two concurrent reassigns swapping the same two clients can deadlock (PG `40P01` → one 500, safe abort, NO corruption). Not a validation blocker; deterministic membership-lock ordering is a queued follow-up.
6. **Docs pass + Q9 resolved** (`f3d0230`) — docs verified against code (code is the source of truth); `docs/roadmap.md` reconciled to the four-layer vision; `.captain/ROADMAP.md` canonical.

**Context:** Founder delegated the recommended reliability work ("do all the recommended in the best order, I trust your decision"). The security exposure checklist from the review is the deploy gate (proxy+TLS, XFF sanitization, Secure cookies, fresh JWT secret, CORS, DB lockdown, retention cron, smoke test).

**Consequences:** Backend requires no further engineering before validation. Next: founder picks provider/region (~$5–10/mo; London/Frankfurt recommended for WA latency) → DevOps stands up the instance per `docs/deployment.md`; API validation kit + Product validation protocol; founder runs the wedge with 1–2 friendly agencies.

---

## 2026-08-14 — Validation path chosen: API-first; founder availability; follow-ups authorized

**Decision:** Q5 RESOLVED — **API-first validation before any frontend.** Q6 RESOLVED — founder available daily/anytime (engineering capacity is agent-assisted; founder drives agency-facing contact). The backend is the core asset and **must be fully reliable before real agencies touch it** (reliability mandate). The three small engineering follow-ups are authorized: `revision_limit` admin setter, provisioned-but-unassigned client removal path, `docs/deployment.md` rollback example fix.

**Context:** Founder decision session 2026-08-14. The wedge is built but unvalidated; the fastest credible validation is API-first with 1–2 friendly agencies using the existing client-approval deep link + one-time credentials (WhatsApp channel). Frontend (M3) is deferred until validation signals justify the build. The reliability mandate re-opens the CI/test-gate deferral for review (Captain recommends shipping the test gate earlier than deploy time).

**Consequences:** Validation sequence = (1) small follow-ups [in progress], (2) reliability pass (close test-coverage gaps, security hardening), (3) minimal deployment for the validation instance (DevOps; provider/budget pending founder), (4) validation kit (API walkthrough, Postman collection, client deep-link demo) + Product validation protocol, (5) founder runs validation with 1–2 friendly agencies. Frontend build is off the near-term roadmap.

---

## 2026-08-14 — Layer-1 delta built and verified; post-review fixes accepted

**Decision:** The Layer-1 delta backend is complete, tested (93 integration tests + unit tests for pure logic), reviewed, and security-reviewed. The independent verification passes found 10 must-fix items; all were fixed and re-verified. Notable post-review fixes:

1. **Client escalation guard completed** — `member.role.update` now rejects role updates when the target membership's **current** role is `client` (403), closing the gap where a client membership could be re-role'd into staff (previously only the `client` target was blocked).
2. **`member.remove` on a client-role membership → 409 `client_attached_to_project`** — the unassign flow is the only client removal path (per Product contract C-5).
3. **Phone-only identity fixed** — `omitempty` added to `Email`/`Phone` in the client-provision DTO **and** the pre-existing auth-register DTO (phone-only registration was broken at HTTP; phone is a primary identity channel in the target market).
4. **Status PATCH blocked on archived/cancelled projects** — generic transitions now load + check the project (design #5).
5. **Transition table completed** — cancel reachable from every non-terminal state (`pending→cancelled`, `in_progress→cancelled`, `blocked→cancelled` added).
6. **Credential-rotation endpoint** `POST /orgs/{orgID}/clients/{userID}/reset-credential` (Security HIGH) — rotates the one-time credential, re-arms `must_change_password`, **revokes all of the client's sessions in the same transaction**, returns the credential once, audited. This was the only HIGH finding; without it a client who lost the one-time credential had no recovery path.
7. **Concurrent double-provision → 409** — both membership-insert paths map the unique violation (was 500 under a double-click).
8. **Client surfaces hide agency-facing revision fields** — milestone detail for client actors omits `limit_reached`/`revision_limit` (consistent with the approval view).
9. **Reassign prunes the displaced client's membership** when they hold no other project (was: only explicit unassign pruned).
10. **Route-table gate invariant enforced by test** — route registration extracted into `cmd/api/routes.go` (data-driven `protectedRoutes` table with a `gated` flag); a test walks the production table and asserts every gated route 403s for a must-change user while `POST /v1/me/password` remains the sole exception.

**Verification:** Tester VERIFIED all 10 fixes (including live HTTP confirmation of session revocation on credential rotation); Reviewer confirmed the review-blocking deviation (transition table) resolved; Security confirmed no remaining must-fix items. Full suite: 93 pass / 0 fail / 0 skip; race-scoped Layer-1 logic clean; migration round-trip verified.

**Context:** The verification gates (test + review + security review) from the 2026-08-14 work order are satisfied. Known minor items remain (documented in CURRENT_STATE.md): no endpoint sets `revision_limit` (schema + read-side wired), provisioned-but-never-assigned clients have no API removal path, `docs/deployment.md` rollback example is stale.

**Consequences:** The backend Layer-1 delta is accepted. Next candidate work: real-agency validation (frontend decision Q5), email delivery + invitation loop + password reset, `revision_limit` admin setter, CI test gate at deploy.

---

## 2026-08-14 — Layer-1 design reconciliation (Captain, from Product + Architect outputs)

**Decision:** Final Layer-1 design choices, reconciling the Product Specialist and Architect deliverables:

1. **API shape — split provisioning from assignment:** `POST /v1/orgs/{orgID}/clients` (provision a client account; permission `client.provision`) + `PUT /v1/orgs/{orgID}/projects/{projectID}/client` (`{client_id | null}`; assign/reassign/unassign; permission `project.client.assign`). Cleaner than a combined endpoint; matches existing org-level/project-level resource patterns.
2. **Existing-user reuse on provision:** if the email/phone matches an existing user, reuse the user (create client membership + assign; return `client_id` with NO credential). If no match, create the user + one-time credential (returned once). Do NOT 409 — reuse matches the Option A "real users" philosophy; clients may already have accounts.
3. **Forced password change on first login:** new `users.must_change_password` flag (default false), set true at provisioning and credential rotation; gated: such users may only change password/logout/refresh until they change it (403 `password_change_required` elsewhere). The one-time credential is agency-visible, so it must be rotated before the client can act. Cleared by the existing `POST /v1/me/password`.
4. **Revision semantics (interpretation of Q12):** `revision_count` = number of **submission rounds** (initial submission = 1, each resubmission = +1); a `milestone_revisions` row is created on every submission (T2/T5) with `submitted_by`/`submitted_at` = the agency actor/time; `revision_limit` (project default, per-milestone override) = maximum submission rounds; `limit_reached = revision_count >= revision_limit`, **computed at read** (never stored) with `limit = COALESCE(milestone.revision_limit, project.revision_limit)`. Note for later copy: "revision" = deliverable version round; flag semantics adjustable later since history is preserved.
5. **State machine surface:** action endpoints `POST .../submit`, `POST .../approve`, `POST .../changes-requested` + generic `PATCH .../status` (staff-only); existing milestone PATCH stays metadata-only (resolves the dead `milestone.status.update` permission-to-route mismatch). Approve is idempotent (no duplicate events on retry); changes-requested is NOT (409 on stale state). `awaiting_approval → completed` is forbidden (client sign-off is the wedge; `cancelled` is the escape hatch). State-machine actions blocked when the project is `archived`/`cancelled`.
6. **RBAC keys (final):** `client.provision`, `client.list`, `project.client.assign`, `milestone.submit`, `deliverable.submit`, `milestone.payment_status.update`, `activity.project.list`. Client role (system template, seed-only): `project.view`, `milestone.view`, `milestone.approve`, `milestone.revision.request`, `activity.project.list`. `milestone.submit` + `deliverable.submit` granted to member (junior staff can upload deliverables). Clients never granted list/org/member keys; excluded from `member.list` at query level; role-update guard in membership service.
7. **Client-facing surface:** shared deep link `GET /v1/orgs/{orgID}/projects/{projectID}/approval` (permission `milestone.view`) returning project + milestones + deliverables + payment status in one call — the WhatsApp landing page; plus project-scoped activity `GET .../projects/{projectID}/activities` (`activity.project.list`).
8. **Migration 000005** includes the opportunistic owner-grant gap fix (000003 never granted owner `project.update`/`milestone.update`/`activity.list`) and the `milestone_status` full column-rewrite (avoiding irreversible `ALTER TYPE ... ADD VALUE`) in BOTH directions.
9. **Deliverables:** table `milestone_deliverables` (url, title, description, submitted_by, submitted_at); ≥1 deliverable required before submit-for-approval (400 `deliverable_required`); duplicates allowed; delete = delete + re-add (no edit).
10. **Payment status:** per-milestone enum unpaid/partial/paid (default unpaid), any→any transitions allowed and audited (`milestone.payment_status_changed`), agency-only (`milestone.payment_status.update`), visible to client read-only.

**Context:** Product Specialist produced the behavior contract (sections A–E); Architect produced the technical design (routes, state machine, migration sketch, enforcement). Points of divergence (endpoint shape, existing-user handling, forced password rotation, revision semantics) resolved here. Design decisions #1–10 supersede any conflicting detail in either specialist output.

**Addendum (Database Specialist validation, same day):** Migration 000005 seeds **9** permission keys (the 7 reconciled keys + `milestone.approve` + `milestone.revision.request`, which were missing from the architect sketch but are required by the client-role grants). Seed fixes bundled: owner regains `project.update`/`milestone.update`/`activity.list` (000001's CROSS JOIN predates them; 000003 never granted them to owner); admin gains `activity.list` (docs/api.md documents it for admin but 000003 granted it only to member/viewer) plus `milestone.submit`/`deliverable.submit` (preserves the documented admin ⊇ member hierarchy). Revision-limit sentinel: `NULL` = no limit, explicit `0` forbidden (`CHECK IS NULL OR >= 1`) — avoids the `0 >= 0` permanently-flagged degenerate. `projects.client_id` FK is `ON DELETE SET NULL`. Down migration is byte-exact (reverses the bundled gap fixes) and data-preserving (`approved → completed`, `changes_requested → awaiting_approval`).

**Consequences:** The Database Specialist validates/refines migration 000005; the Planner sequences the 10-phase build; the Builder implements; Tester/Reviewer/Security Engineer verify per the approved work order.

---

## 2026-08-14 — Layer-1 delta approved: wedge, geography, and design sub-decisions confirmed

**Decision:** Founder approved the reframed wedge and confirmed the target market; Layer-1 build is authorized. Design sub-decisions locked:

1. **Wedge approved (Q2 RESOLVED):** milestone-level sign-off → revision limits → invoicing → payment status for 2–20 person agencies, especially with African payment rails. This is the design basis for the Layer-1 delta.
2. **Geography confirmed (Q1 RESOLVED):** Nigeria/West Africa is the working market (upgraded from research-supported to founder-confirmed). Paystack/Flutterwave rails, WhatsApp client channel, Nigerian WHT requirements apply.
3. **Client provisioning (Q11a):** clients are provisioned by the agency with a **one-time credential** returned in the invite response and shared out-of-band (WhatsApp fits the market). No email provider needed for MVP; email delivery slots in later.
4. **Client visibility (Q11b):** clients are **project-scoped only** — narrow permission keys (project.view, milestone.view, milestone.approve, milestone.revision.request), never in member.list or org-wide activity feed; project-scoped activity view only.
5. **Revision semantics (Q12):** **track + flag, no hard cap** — `revision_count` + `milestone_revisions` history + configurable per-project/milestone limit; a `limit_reached` flag surfaces over-revision to the agency without blocking the client's approval path.
6. **Payment status (Q13):** **per-milestone** status (unpaid/partial/paid), agency-updated, display-only, no money movement; visible on the client approval view.

**Context:** All prior Layer-1 design (client modeling as real user + membership + `client` system role, `projects.client_id`, approval state machine reusing `awaiting_approval`, versioned audit events, AI-readiness fix set) carries forward unchanged.

**Consequences:** Work order for the build: (1) Product requirements, (2) Architecture design, (3) Database migration design, (4) Implementation plan, (5) Build, (6) Test + review + security review. The Layer-1 delta includes the previously-agreed AI-readiness fixes (activity metadata bug, versioned audit metadata convention, `audit_logs(entity_type, entity_id, created_at)` index, audit row per state transition).

---

## 2026-08-13 — Boundary hardening implemented and verified

**Decision:** Enforce organization boundaries consistently for project updates and nested milestone/assignment lists. Project updates now use an organization-scoped SQL predicate and a transaction-aware scoped `FOR UPDATE` read before mutation; nested list services validate the parent project in the organization; assignment uses a narrow project lookup dependency.

**Verification:** Same-org success, cross-org/missing-parent rejection, valid empty parents, and HTTP behavior are covered by tests. Tester ran the full, focused live-PostgreSQL suites (including race tests); all passed with no integration tests skipped. Reviewer approved with no findings.

**Remaining coverage gaps:** No dedicated HTTP project cross-org GET/PATCH tests, no dedicated HTTP milestone cross-org test, and no live concurrency-lock test. These gaps do not change the completed implementation status.

**Verification:** Same-org success, cross-org/missing-parent rejection, valid empty parents, and HTTP behavior are covered by tests. Tester ran the full and focused live-PostgreSQL suites, including the focused race run; all passed with no integration tests skipped. Reviewer approved with no findings.

**Remaining coverage gaps:** No dedicated HTTP project cross-org GET/PATCH tests, no dedicated HTTP milestone cross-org test, and no live concurrency-lock test. These gaps do not change the completed implementation status.

---

## 2026-08-13 — Competitive research completed; wedge reframed (Proposed, pending founder approval)

**Decision (proposed):** Adopt the research-reframed wedge: *"client-flow incumbents (HoneyBook/Dubsado/Bonsai) are thin on milestone-level coordination, and no one in any segment connects milestone sign-off → revision limits → invoicing → payment status for small agencies — especially with African payment rails."* The original claim ("incumbents are thin on milestone coordination" as a blanket) was disproven by Moxie/Accelo/Function Point.

**Context:** Researcher validated the Layer-1 concept but found the initial positioning overbroad. Also: Bonsai was acquired by Zoom and closed (Dec 2025); HoneyBook is US/CA/UK/AU only (absent from the likely Nigerian market).

**Consequences:** Layer-1 differentiation concentrates on: milestone-level sign-off (not task-level), revision-round limits (thin everywhere), approval-gate → invoice → payment-status linkage, client-facing audit trail, 2–20 person agency segment, Paystack/Flutterwave + WhatsApp rails.

---

## 2026-08-13 — Geography assumption upgraded to research-supported (awaiting founder confirmation)

**Decision:** Treat Nigeria/West Africa as the working-market hypothesis. **Status: supported by evidence, not yet confirmed by founder.**

**Context:** Paystack/Flutterwave are the default rails; WhatsApp is the client channel; Nigerian WHT (5%/10%) is a real invoicing requirement; US incumbents are absent or lack local payment rails; a local invoicing-first competitor field exists (WikrenaOS, StratiSell, startbuddi, Bill.i.ng, CRM Africa).

**Consequences:** Financial-layer provider choices will be Paystack/Flutterwave class; workflow-first is differentiated locally; WHT handling may be a required invoicing feature.

---

## 2026-08-13 — Tenant-isolation hole FIXED (implemented)

**Decision:** Fix executed. Project/milestone read paths are now org-scoped; the unscoped `Repository.GetByID`/`GetMilestoneByID` methods were deleted; `ListMilestonesByProjectID` filters by `project_id AND organization_id`; handlers pass `requestctx.OrganizationID`. Regression test `TestProjectReadsScopedToOrg` added (cross-tenant reads return not-found, no existence leak). Build/vet clean; full integration suite passes.

**Context:** Implements the "fix is a prerequisite" decision above. The scoped repo methods and `ensure*Access` helpers already existed for the write paths — the read paths simply bypassed them.

**Consequences:** The documented query-level isolation guarantee now holds for all project/milestone access. Client-role feature prerequisite satisfied. When the client feature lands, project-level (`client_id`) scoping builds on top of this. (Uncommitted as of this entry — commit pending founder's word.)

---

## 2026-08-13 — Architectural audit: tenant-isolation hole found; fix is a prerequisite (Proposed)

**Decision (proposed):** Fix the org-scoping hole in `project.Repository.GetByID`, `GetMilestoneByID`, and `ListMilestonesByProjectID` **before** the client-role feature is built.

**Context:** Independent architectural audit found project/milestone read-by-ID paths filter by ID/project_id only, with no organization filter — contradicting the documented query-level isolation guarantee. Any active member of any org can read any project/milestone by guessing a UUID. This is a security defect, not a design choice (the assignment package is correctly scoped).

**Consequences:** Fix lands in the Layer-1 milestone. `docs/architecture.md:87-88` overstates the isolation guarantee and should be corrected when the fix lands. This also invalidates CURRENT_STATE's earlier "no material structural debt" claim (corrected).

---

## 2026-08-13 — Client modeling: real user + membership + `client` system role (Proposed)

**Decision (proposed):** Model clients as real `users` rows + `memberships` rows + a new `client` system template role, with `projects.client_id` for visibility and service-layer project-scope enforcement. Do **not** build a separate `clients` table.

**Context:** Architect's analysis: a separate client entity would fragment identity, auth middleware, session validation, and the audit substrate (all keyed on `users.id`). Option A reuses the entire auth stack. Costs: invite flow must gain a "provision + one-time credential" path; `password_hash NOT NULL` forces credential provisioning; must guard `member.role.update` against escalating client memberships.

**Consequences:** Client role gets narrow keys only (project.view, milestone.view, milestone.approve, milestone.revision.request); never list/activity/member/org keys. See ARCHITECTURE.md.

---

## 2026-08-13 — AI-readiness: minimal fix set, no event infrastructure (Proposed)

**Decision (proposed):** Achieve AI-readiness with five small fixes, not speculative infrastructure: (1) fix `activity.NewActivity` metadata drop; (2) standardize Layer-1 audit events with versioned metadata (`{"schema_version": 1, "before", "after", "reason"}`) and stable action keys (`milestone.approved`, `milestone.changes_requested`, `milestone.revision_submitted`, `deliverable.submitted`, `milestone.payment_status_changed`); (3) add `audit_logs(entity_type, entity_id, created_at)` index; (4) milestone state machine writes an audit row on every transition (that row IS the status-transition history); (5) `milestone_revisions` table serves as revision history. No event bus/outbox/Kafka/event store.

**Context:** Architect audit found the AI-readiness claim in the memory overstated: activity metadata dropped, audit metadata inconsistent, no transition history, no per-entity index.

**Consequences:** One migration + one convention document + state-machine service code. Fold into the Layer-1 build.

---

## 2026-08-13 — Removed the dead `REQUIRE_EMAIL_VERIFICATION` flag and gate

**Decision:** Delete the `REQUIRE_EMAIL_VERIFICATION` config flag, the gate in `ChangePassword`, the `ErrEmailNotVerified` error, its HTTP mapping, and the `GetEmailVerified` repository method. Commit `6f1dc28`. The `email_verified` column, model field, and profile response field remain.

**Context:** The flag was wired into `ChangePassword` but no email-verification flow exists anywhere in the codebase (no endpoint, no OTP, no way to set `email_verified = true`). Flipping the flag would permanently break all password changes. Dead config that looked live.

**Alternatives considered:**
1. Remove flag + gate entirely (chosen)
2. Keep gate but hide flag from docs until flow exists
3. Build email verification now

**Reason:** Config surface must be truthful; cheapest credible cleanup; the feature will be rebuilt together with the verification flow when it ships.

**Consequences:** `email_verified`/`phone_verified` columns stay dormant in schema. When email verification is built, flag + gate + error return together with the flow. Cleanup verified: build/vet clean, 40/40 integration tests pass.

---

## 2026-08-13 — CI deferred to deploy time (founder decision)

**Decision:** No CI pipeline now. CI will be set up when the project is ready to deploy, as part of infrastructure work (devops agent).

**Context:** All 10 test files are integration tests requiring a live PostgreSQL (`EGTEST_DB_URL`). No `.github/workflows` exists. Risk: tests only run when a developer with a local DB runs them.

**Alternatives considered:**
1. Defer CI to deploy time (chosen by founder)
2. Add a minimal GitHub Actions test gate now (one YAML + Postgres service container, ~half a day) — recommended by Architect and Captain; **not accepted**

**Reason:** Founder's call — infrastructure belongs with the deploy/devops phase.

**Consequences:** Risk of silent regressions in the auth/RBAC hardening remains until deploy time. The recommendation stands; revisit when deploy work starts. The Captain's position: defer the *deploy pipeline*, but consider shipping the *test gate* earlier. Still open to discussion.

---

## 2026-08-13 — Four-layer product vision established (workflow → operations → financial → intelligence)

**Decision:** Egentop's product direction is the four-layer vision: (1) Workflow — projects, milestones, approvals, revisions, audit; (2) Operations — automation, templates, notifications, reporting; (3) Financial — invoicing, milestone payments, escrow, payouts; (4) Intelligence — AI agents. AI is explicitly **not** the MVP. Escrow is a later feature, not the product. First market: agencies (software/branding/design/marketing/creative), then other service businesses.

**Context:** Founder's vision statement, 2026-08-13. Replaces the implicit "generic PM SaaS" framing that the existing codebase and `docs/roadmap.md` reflect.

**Alternatives considered:** Escrow-first (rejected — trust/compliance/capital); generic PM SaaS (rejected — no wedge); agent-native product now (rejected — AI needs workflow data first).

**Consequences:** Layer-1 product delta (approvals, revisions, client role, deliverables, payment status) becomes the build target. `docs/roadmap.md` is now legacy and needs reconciliation (see OPEN_QUESTIONS.md). The existing audit/activity/RBAC substrate aligns well with Layer 1.

---

## 2026-08-13 — Layer-1 technical approach (Proposed, not yet committed)

**Decision:** Build Layer-1 delta as: a **client** role in RBAC (narrow scope); an **approval state machine** on milestones (`pending_approval → approved / changes_requested`); **revision tracking** (counter + history); **link-based deliverables** first (Figma/Drive/docs URLs), file upload later; **payment status** tracking per milestone (unpaid/partial/paid) with no money movement.

> **Superseded in part (2026-08-13):** the approval-state naming `pending_approval` is superseded — see the later entries "Client modeling" and the ARCHITECTURE.md note: reuse the existing `awaiting_approval` enum value and add `approved` + `changes_requested`. The client-role approach here is also superseded by the "Client modeling: real user + membership + `client` system role" entry below.

**Status: Proposed.** Pending: competitive research (HoneyBook/Dubsado/Moxie/Bonsai) to validate the wedge, and founder approval.

**Context:** The vision's Layer 1 requires these capabilities; the backend already has orgs/memberships/projects/milestones/activity/audit/RBAC.

**Reason:** Milestone-level coordination (approvals/revisions) is the believed differentiator vs. invoicing-first competitors; link-based deliverables avoids building storage infrastructure prematurely.

**Consequences:** New migration(s) for milestone status/approval/revision/payment fields + new permissions (e.g. `milestone.approve`). Must keep new events flowing into the existing audit/activity substrate (AI-readiness).

---

## 2026-08-13 — Layer-3 sequencing: invoicing/payment tracking before escrow (Proposed)

**Decision:** When the financial layer arrives, start with milestone invoicing and payment *tracking*, not escrow. Escrow is the last financial step.

**Context:** Escrow is three-party: the agency must trust the platform (Layer-1/2 build this) but the **client** must also trust it — the vision's trust narrative is agency-side only. That asymmetry is a known gap.

**Reason:** Agency-facing invoicing needs no client-side trust; escrow requires clients to have interacted with the system first.

**Consequences:** Financial layer has two phases (tracking/invoicing → escrow/payouts). Payment provider choice depends on target geography (Paystack vs Stripe class) — geography is an open question.

---

## Established pre-conversation decisions (Confirmed by code review, recorded 2026-08-13)

These were made before this knowledge base existed; they are confirmed by reading the code and are recorded for durability:

- **JWT access tokens + rotating HttpOnly refresh cookies with server-side session persistence** — sessions table, token families, reuse detection → family revocation, bcrypt storage, per-request session validation. (Auth is the most hardened part of the system.)
- **Data-driven RBAC** with system template roles (owner/admin/member/viewer) and permission keys; single-query checks; every decision audited to `authz_decisions`.
- **Query-level tenant isolation** with `organization_id` scoping; middleware as a second layer.
- **Clean architecture** (handler → service → repository), feature packages under `internal/`, sentinel errors + centralized HTTP error mapping.
- **PostgreSQL with up/down SQL migrations**, `update_updated_at_column()` triggers, savepoint-based slug retry inside transactions.
- **Email and/or phone identity** for users (unique nullable columns, CHECK at least one); auto-created default organization with owner membership at registration.
- **Audit everything, in-transaction** (audit_logs + authz_decisions + activity feed).
