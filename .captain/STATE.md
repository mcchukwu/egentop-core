# Egentop — Captain State Briefing

> Concise current briefing. Snapshot, not canonical truth. Canonical detail lives in the other .captain/ files.
> Last updated: 2026-08-14 (reliability pass complete)

## Current Objective

Validate the wedge (milestone sign-off → revision limits → payment status) with real agencies, **API-first**, on a fully reliable backend. Frontend is deferred until validation signals justify the build.

## Current Phase

Post-MVP backend → **API-first validation**. Layer-1 delta + **reliability pass are COMPLETE** (commits `3185247` → `f3d0230`; 134 tests, 0 failures; CI gate live; security-reviewed; reviewed). **Next: stand up the minimal validation deployment** (founder picks provider/region) and run the wedge with 1–2 friendly agencies.

## Active Work

- **Reliability pass: DONE** — security exposure review (no criticals); reliability batch (`b536853`: revision_limit setter, client removal endpoint, getClientIP hardening, error-mapper 400s, login 401 unification, 1MB body limit, LOG_LEVEL wiring); **race fix** (`9606e7b`, Database Specialist design — assign/remove serialized on the membership row; Tester-verified 134/134, Reviewer APPROVED); CI test gate shipped (`817c0c1`); deployment artifacts + runbook (`817c0c1`); docs verified against code + roadmap reconciled — Q9 RESOLVED (`f3d0230`).
- **Validation readiness (NEXT):** founder picks provider/region (~$5–10/mo; London/Frankfurt recommended for WA latency) → DevOps stands up the instance per `docs/deployment.md` runbook + smoke checklist. API validation kit (wedge walkthrough, Postman collection, client deep-link demo) and Product validation protocol not yet built.
- **Founder:** line up 1–2 friendly agencies to run the wedge.

## Current Priorities

1. Validation deployment (founder provider/region decision is the gate)
2. API validation kit + Product validation protocol
3. Founder: recruit 1–2 friendly agencies and run validation
4. Feed validation signals into pricing (Q4), distribution (Q3), frontend decision (Q5)
5. Backend is reliable and stable — no further engineering needed before validation

## Current Blockers

- **None technical.** 134/134 tests, race-free, CI gated, review-approved.
- Validation deployment awaits founder provider/region choice (~$5–10/mo class).
- Agency recruitment is founder-side.

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

Founder: choose the validation provider/region. Then DevOps stands up the instance per the runbook + smoke checklist, and the validation kit + protocol are built while agencies are recruited. No further backend engineering is required before validation.

## Recent Changes

- 2026-08-14: **Reliability pass complete** — security review → reliability batch (`b536853`) → race fix (`9606e7b`, Tester-found, DB-specialist-designed, Reviewer-approved) → docs pass + Q9 resolved (`f3d0230`). 134/134 tests. CI gate + deployment artifacts (`817c0c1`).
- 2026-08-14: Q5 resolved (API-first validation before frontend); Q6 resolved (founder daily availability); small follow-ups authorized; reliability mandate stated
- 2026-08-14: Layer-1 delta committed (`3185247`) — client role, approval state machine, revisions, deliverables, payment status, AI-readiness fixes
