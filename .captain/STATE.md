# Egentop — Captain State Briefing

> Concise current briefing. Snapshot, not canonical truth. Canonical detail lives in the other .captain/ files.
> Last updated: 2026-08-14

## Current Objective

Validate the wedge (milestone sign-off → revision limits → payment status) with real agencies, **API-first**, on a fully reliable backend. Frontend is deferred until validation signals justify the build.

## Current Phase

Post-MVP backend → **API-first validation**. Layer-1 backend complete (committed `3185247`, 93 integration tests, reviewed, security-reviewed). Small engineering follow-ups in progress. Reliability pass + validation deployment next.

## Active Work

- Builder (manual-invoke): follow-up batch — `revision_limit` admin setter, provisioned-but-unassigned client removal path, **plus recommended cheap security fixes from the exposure review** (XFF rate-limit key hardening, error-mapper 400s, unified login 401, MaxBytesReader, dead rate-limit config cleanup). Authorized base items; security batch pending founder OK on scope.
- Security validation-exposure review: **DONE 2026-08-14** — no criticals; HIGH-1 XFF trust, MEDIUM-1/2/3, MEDIUM-4 accepted for validation; exposure checklist is the deploy gate (see CURRENT_STATE.md).
- Documenter: `docs/deployment.md` rollback example fix — **DONE** (uncommitted); nginx sample config in same file needs fixing at deploy (XFF).
- Up next: Tester coverage-gap closure (HTTP cross-org GET/PATCH, live concurrency-lock test); DevOps minimal validation deployment (needs founder provider/budget; sanitizing proxy + TLS are hard requirements).

## Current Priorities

1. Backend reliability (founder mandate — the backend is the core asset)
2. Small follow-ups (approved)
3. Reliability pass (test coverage, security hardening, CI test gate decision)
4. Validation readiness (deployment, API kit, validation protocol)
5. Founder: line up 1–2 friendly agencies and run validation

## Current Blockers

- None technical.
- Validation deployment needs founder provider/budget choice (~$5–10/mo class).
- CI test gate re-review pending (previously deferred to deploy time; reliability mandate re-opens it).

## Important Risks

- Silent regressions: no CI test gate; the 93 integration tests only run when someone runs them with live Postgres
- Rate-limit bypass if the API is exposed without a sanitizing proxy (X-Forwarded-For trust) — Security review confirmed; mitigation = proxy overwrites/strips XFF + cheap code hardening (recommended Builder batch); sanitizing proxy + TLS are non-negotiable deploy requirements
- Core business assumption (workflow clarity → cash flow) still unvalidated with real users
- `authz_decisions` grows unbounded without cleanup automation (deploy-time cron)
- First real agencies will hit the API — any reliability bug becomes a trust issue

## Open Decisions

- Q3 distribution channel (validation signals will inform)
- Q4 pricing model (validation signals will inform)
- Q7 escrow trust asymmetry (Layer 3 sequencing, proposed)
- Q8 AI layer trigger
- Q9 legacy docs reconciliation (`docs/roadmap.md`)
- Q10 email provider (deploy-time)
- CI test gate now vs deploy time (re-review recommended)

## Next Recommended Action

Invoke Builder (manual) with the follow-up batch (two authorized items + recommended cheap security fixes), then Tester coverage-gap closure, then present the validation-deployment plan (DevOps) with the security exposure checklist as the gate for founder provider/budget sign-off.

## Recent Changes

- 2026-08-14 (resume): Session resumed as Captain. `go build ./...` + `go vet ./...` clean on the working tree. Documenter's `docs/deployment.md` rollback fix verified complete (uncommitted). No AGENTS.md present; `.captain/` + `docs/deployment.md` uncommitted.
- 2026-08-14: Q5 resolved (API-first validation before frontend); Q6 resolved (founder daily availability); small follow-ups authorized; reliability mandate stated
- 2026-08-14: Layer-1 delta committed (`3185247`) — client role, approval state machine, revisions, deliverables, payment status, AI-readiness fixes
