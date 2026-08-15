# Egentop — Captain State Briefing

> Concise current briefing. Snapshot, not canonical truth. Canonical detail lives in the other .captain/ files.
> Last updated: 2026-08-15 (FE-O2 implemented; frontend build next)

## Current Objective

Validate the wedge (milestone sign-off → revision limits → payment status) with real agencies, **API-first**, on a fully reliable backend. Frontend is deferred until validation signals justify the build.

## Current Phase

Post-MVP backend → **API-first validation**. Layer-1 delta + **reliability pass are COMPLETE** (commits `3185247` → `f3d0230`; 134 tests, 0 failures; CI gate live; security-reviewed; reviewed). **Next: stand up the minimal validation deployment** (founder picks provider/region) and run the wedge with 1–2 friendly agencies.

## Active Work

- **Frontend (M3) — APPROVED + initialized 2026-08-14** in a separate codebase at `/home/miracle/projects/egentop-frontend`: requirements, stack research, architecture, diagrams, `.captain/` memory all done + committed. Q5 ("API-first validation before any frontend") **superseded the same day** — the founder correctly identified agencies can't operate a raw API (see DECISIONS.md).
- **Backend: complete and validation-ready** — no further backend engineering required before validation (134/134, CI-gated, race-free, reviewed).
- **FE-O2 DONE (2026-08-15, commit `42d0875`)** — `revision_limit` now on project list/detail payloads for staff actors; client surfaces exclude it (handler role split + direct-handler test, defense in depth). 136/136 tests (was 134). Sign-offs FE-O1/FE-O2/FE-O3 complete; deployment region (London/Frankfurt) confirmed; provider pending DevOps.

## Current Priorities

1. Validation deployment (founder provider/region decision is the gate)
2. API validation kit + Product validation protocol
3. Founder: recruit 1–2 friendly agencies and run validation
4. Feed validation signals into pricing (Q4), distribution (Q3), frontend decision (Q5)
5. Backend is reliable and stable — no further engineering needed before validation

## Current Blockers

- **None technical.** 134→136/136 tests, CI-gated, review-approved.
- All founder sign-offs complete (2026-08-15); FE-O2 landed. Next gate: Planner decomposition → frontend build session.

## Important Risks

- Core business assumption (workflow clarity → cash flow) still unvalidated with real users — the entire point of this phase
- First real agencies will hit the API — deployment MUST follow the runbook smoke checklist (TLS, sanitized proxy, Secure cookies, fresh JWT secret, DB lockdown, retention cron)
- Residual deadlock on symmetric concurrent client-swap reassigns (rare, PG 40P01, one 500, NO corruption) — documented; deterministic lock ordering is a queued follow-up
- Test-suite fixture leaks in pre-existing stress/provision tests (no t.Cleanup convention) — housekeeping item
- `authz_decisions` cleanup cron only exists as artifacts — must be installed at deploy

## Open Decisions

- Q3 distribution channel (validation signals will inform)
- Q4 pricing model (validation signals will inform)
- Q7 escrow trust asymmetry (Layer 3 sequencing, proposed)
- Q8 AI layer trigger
- Q10 email provider (deploy-time)
- **Q9 legacy docs: RESOLVED 2026-08-14** (roadmap reconciled; .captain/ROADMAP.md canonical)
- **CI test gate: DECIDED (Captain) — shipped now** (`817c0c1`)
- Deployment provider/region: **open — founder picks**; London/Frankfurt recommended

## Next Recommended Action

FE-O2 DONE (`42d0875`). Next: Planner decomposes the frontend build in `/home/miracle/projects/egentop-frontend` (stack locked: Svelte 5 + SvelteKit 2 — FE-O6) → Builder executes phases → deploy frontend + API + CORS env + smoke (DevOps; region London/Frankfurt) → real-device WhatsApp session test → run validation with 1–2 friendly agencies. Backend requires no further engineering before validation.

## Recent Changes

- 2026-08-14: **Frontend approved + initialized** (`egentop-frontend`, commit `b458f07`) — requirements, research, architecture, diagrams, project memory. Q5 superseded (founder: agencies can't operate a raw API; interface is on the validation critical path). Backend DECISIONS/ROADMAP/OPEN_QUESTIONS updated.
- 2026-08-14: **Reliability pass complete** — security review → reliability batch (`b536853`) → race fix (`9606e7b`, Tester-found, DB-specialist-designed, Reviewer-approved) → docs pass + Q9 resolved (`f3d0230`). 134/134 tests. CI gate + deployment artifacts (`817c0c1`).
- 2026-08-14: Q5 resolved (API-first validation before frontend); Q6 resolved (founder daily availability); small follow-ups authorized; reliability mandate stated
- 2026-08-14: Layer-1 delta committed (`3185247`) — client role, approval state machine, revisions, deliverables, payment status, AI-readiness fixes
