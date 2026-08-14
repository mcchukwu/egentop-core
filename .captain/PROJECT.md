# Egentop — Project Context

> Source of truth for the project. Read this first before any task.
> Last updated: 2026-08-13

## Project Name

Egentop (codebase: **Egentop-Core**, module `github.com/mcchukwu/egentop`).
The name reads as "agent-top" and is intentional: the intelligence/agent layer is the top layer of the product vision.

## One-Sentence Description

Egentop is an AI-powered operations platform for service businesses; it starts as a project workflow + approvals system for agencies and grows into an operating system covering operations, financial infrastructure, and AI-assisted coordination.

## Problem Being Solved

Service businesses (agencies first) run client projects by stitching together WhatsApp, email, Google Docs, Excel, bank transfers, PDF invoices, and memory. Consequences:

- Nobody knows exactly what was agreed
- Approvals happen on WhatsApp (no record)
- Revisions become unlimited
- Milestones aren't clearly defined
- Clients disappear; agencies chase payments
- Everyone argues about what "finished" means

Core diagnosis (founder's framing, Confirmed): **money is delayed because workflow is unclear.** The system doesn't move money at first — it coordinates work, which is the easier first sale and the cause behind the visible payment pain.

## Why the Project Exists

The original idea was an escrow platform ("Paystack + Escrow"). That was rejected because escrow requires trust, compliance, legal work, payment integrations, dispute handling, security, and capital — and people won't trust a platform with money they don't already trust. So the entry point became: "Can we help you manage your projects better?" instead of "Can we hold your money?"

## Target Users

- **Initial (Confirmed):** software agencies, branding agencies, design studios, marketing agencies, creative studios
- **Later (Planned):** consulting firms, architecture firms, legal firms, construction companies, logistics businesses, engineering firms — any business delivering work in stages
- **Geography (founder-confirmed, 2026-08-14):** Nigeria/West Africa. Evidence: Paystack/Flutterwave are the default payment rails; WhatsApp is the de facto client channel; Nigerian withholding tax (5% services / 10% consultancy) is a real invoicing requirement; US incumbents are absent (HoneyBook is US/CA/UK/AU only; Stripe-based tools don't serve local collection). A local competitive field exists (invoicing-first: WikrenaOS, StratiSell, startbuddi, Bill.i.ng, CRM Africa).

## Competitive Landscape (Researched 2026-08-13; wedge approved by founder 2026-08-14)

The wedge was validated but **reframed**. The blanket claim "incumbents are thin on milestone coordination" is false — Moxie (task-level client approval), Accelo and Function Point (enterprise sign-offs/proofing) do it. The approved, defensible wedge:

> **The client-flow incumbents (HoneyBook, Dubsado, Bonsai) are thin on milestone-level coordination, and no one in any segment connects milestone sign-off → revision limits → invoicing → payment status for small (2–20 person) agencies — especially with African payment rails.**

Key competitor facts (primary-sourced):
- **HoneyBook** (US/CA/UK/AU only): milestones as timeline blocks + milestone payments; **no native deliverable sign-off**, no revision tracking. $29–$109/mo.
- **Dubsado**: **no milestones, no approval workflow at all**; document hub portal. ~$28–$44/mo.
- **Moxie** (ex-17hats): **does have native client approval** (kanban task sign-off, approval history, webhook); no revision tracking. $10–$32/mo.
- **Bonsai**: acquired by Zoom, closed Dec 2025; phases not shipped; ZoomMate/MCP integration path.
- **Accelo / Function Point**: real sign-offs/proofing + (FP) revision tracking — but enterprise-PSA segment, different buyers.

Local (Nigeria) competitors are invoicing/payment-chasing-first; workflow-first (approvals/revisions/audit) is differentiated locally but the field is real and moving.

AI in competitors (for the later Layer 4): all ship **admin/communication AI** (drafting, summaries, automation builders, meeting-to-CRM). **None ship AI on the delivery surface** (deliverable review vs scope, revision summarization, approval-package generation) — that space remains open.

Research limitations: local-tool user counts/willingness-to-pay unverified; some pricing triangulated from secondary sources. See CURRENT_STATE.md for the full brief.

## Core Goals (Four-Layer Vision — Confirmed product direction, 2026-08-13)

1. **Workflow Layer** — Projects, milestones, approvals, revisions, audit history
2. **Operations Layer** — Automation, templates, notifications, reporting, team collaboration
3. **Financial Layer** — Invoicing, milestone payments, escrow, payouts, reconciliation
4. **Intelligence Layer** — AI agents that plan, monitor, summarize, detect risks, and eventually coordinate work autonomously

Sequencing principle: each layer only becomes natural after the layer below generates the trust/data it needs. **AI is not the MVP** — it comes after workflow data exists.

## Non-Goals / Explicit Exclusions

- **No money movement in Layer 1** — the workflow platform only tracks payment *status*
- **No escrow-first launch** — escrow becomes a feature later, not the product
- **No generic PM SaaS** competing head-on with Linear/Asana/ClickUp/Notion (no wedge, no distribution)
- **No AI features before workflow data** — AI without structured workflow data is a demo, not a product
- Email verification flow does not exist and is not being built now (deferred; column dormant)

## Core Capabilities (Built — the current backend)

> **Important nuance:** the current codebase *is* a generic multi-tenant PM backend — that is the Layer-1 workflow substrate. The product is *not* being positioned as a generic PM SaaS (see Non-Goals); the vision repurposes this substrate as the agency workflow/approvals platform. Do not add generic PM features "because the code is a PM backend" — the product direction governs what gets built.

- Authentication: register (email and/or phone), login, refresh, logout, logout-all
- Password change with revocation of all other sessions
- User profile (view/update)
- Organizations: create, list, view, update; auto-created default org on registration
- Memberships: add, invite by email, list, role update, remove
- Projects, Milestones, Assignments: create, list, view, update (+ assignment remove)
- Activity feed per organization
- RBAC with system template roles (owner/admin/member/viewer) and permission keys
- Audit log + authz-decision recording
- Session persistence with refresh-token rotation and reuse detection
- Rate limiting on auth endpoints
- Health endpoints: `/v1/health`, `/v1/ready`, `/v1/live`

## Important Domain Concepts & Terminology

| Term | Meaning |
|---|---|
| Organization | The tenant; scopes all business data |
| Membership | User ↔ organization link, with a status (active/invited/suspended) and a role |
| Role / Permission | Data-driven RBAC; roles are named sets of permission keys; system template roles have `organization_id IS NULL` |
| Project / Milestone / Assignment | Core work entities under an organization |
| Session / Token family | A refresh-token lineage; rotating a token keeps the family, reusing a revoked token revokes the whole family (theft signal) |
| Audit log | `audit_logs` — business events written in the same transaction as the mutation |
| Authz decision | `authz_decisions` — every permission check, allowed or denied, with reason |
| Activity feed | Denormalized human-readable feed for the UI |

## High-Level System Description

Multi-tenant REST API (`/v1`), clean architecture: **handler → service → repository → PostgreSQL**. Requests flow through a middleware chain (recovery → request ID → logging → security headers → CORS → rate limit → auth → org load → org access → RBAC). Tenant isolation enforced at the query level via `organization_id` scoping from request context. Everything is a self-contained package under `internal/`. See ARCHITECTURE.md.

## Technology Choices (Confirmed by code)

| Layer | Choice |
|---|---|
| Language | Go 1.26+ |
| Database | PostgreSQL (compose: `postgres:18`) |
| Migrations | golang-migrate-style SQL, up/down pairs |
| Auth | `golang-jwt/jwt/v5` (HMAC), bcrypt cost 12 |
| DB driver | `pgx/v5` (via stdlib) |
| Validation | `go-playground/validator/v10` |
| Containerization | Docker Compose |
| HTTP | Standard library `net/http` with Go 1.22+ method/pattern routing |

## Important Constraints

- Multi-tenant data isolation is a hard requirement — enforced at query level, never trusted to middleware alone. **Known gap (2026-08-13 audit): project/milestone read-by-ID paths are not org-scoped — see CURRENT_STATE.md Known Problems**
- Every authz decision is audited (this is a product feature, not just logging)
- All audit writes happen inside the same transaction as the mutation
- Config surface must be truthful: every exposed flag must actually work (see DECISIONS.md — a dead flag was removed for this reason)
- No email delivery, object storage, or payment provider integrated yet
- Refresh tokens are HttpOnly cookies; `Secure` flag set in production

## Important Assumptions

- Agencies' primary pain is workflow clarity, and fixing it improves cash flow (this is why they'd pay) — **Assumption, not yet validated with users**
- Agencies will adopt a workflow tool quickly because it doesn't move money — **Assumption**
- Geography: Nigeria/West Africa — **founder-confirmed (2026-08-14)** — see DECISIONS.md
- Escrow later requires both agency *and* client trust in the platform — the vision's trust narrative is agency-side only (**known gap, see OPEN_QUESTIONS.md**)
- Revision-round limits + milestone-level sign-off connected to invoicing are the differentiator (not generic PM) — **reframed by competitive research, approved by founder 2026-08-14**

## Current Project Phase

**Post-MVP backend; Layer-1 backend complete; API-first validation (2026-08-14).** The backend MVP is complete and hardened. The four-layer product vision was formally established 2026-08-13. Competitive research completed 2026-08-13 (wedge validated + reframed); founder approved the reframed wedge and confirmed Nigeria/West Africa as the market on 2026-08-14. The Layer-1 delta backend (client role, milestone approvals, revisions, deliverables, payment status + AI-readiness fixes) was built, tested (93 integration tests), reviewed, and security-reviewed 2026-08-14. The critical tenant-isolation hole found by the 2026-08-13 audit was fixed 2026-08-13 (prerequisite for the client-role feature satisfied). **Phase: API-first validation** — founder approved API-first validation before any frontend (Q5) and confirmed daily availability (Q6) on 2026-08-14; small engineering follow-ups in progress; reliability pass and a minimal validation deployment are next. No frontend exists. No real users yet. Next gate: running the wedge with 1–2 friendly agencies.

## Memory Files

- [ARCHITECTURE.md](ARCHITECTURE.md) — technical architecture, confirmed vs proposed vs unknown
- [DECISIONS.md](DECISIONS.md) — append-only decision record
- [CURRENT_STATE.md](CURRENT_STATE.md) — what is done, what is in progress, known problems
- [ROADMAP.md](ROADMAP.md) — vision, milestones, near/medium/future/deferred work
- [OPEN_QUESTIONS.md](OPEN_QUESTIONS.md) — unresolved questions and risks

## Quick Orientation for a New Engineer

1. Read ARCHITECTURE.md and `docs/architecture.md` (canonical doc) for the layered design.
2. Read `cmd/api/main.go` for the route table and middleware wiring.
3. Migrations live in `migrations/` (numeric order, up/down pairs).
4. Feature packages under `internal/` each expose handler/service/repository/model/dto.
5. Integration tests require a live PostgreSQL (`EGTEST_DB_URL`) and are the only tests that exist.
6. Note: `docs/roadmap.md` predates the current product direction and needs reconciliation — see OPEN_QUESTIONS.md.
