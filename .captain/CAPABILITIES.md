# Egentop — Capability Map

> Current AI organization: available agents, their domains, available skills, and gaps.
> Last updated: 2026-08-14

## Agents

| Agent | Domain ownership | Notes |
|---|---|---|
| Captain | Strategy, prioritization, orchestration, synthesis, project memory | This role |
| Product | Requirements, scope, acceptance criteria, validation protocol | Needed for the validation protocol |
| Researcher | External facts, technology research | Used for competitive research 2026-08-13 |
| Architect | System architecture | Layer-1 design complete |
| Database Specialist | Persistence, schema, migrations, data integrity | Migration 000005 validated; consult for any new migration |
| Security Engineer | Threats, vulnerabilities, remediation | Delta security-reviewed 2026-08-14; needed for validation-exposure review |
| DevOps | Infrastructure, deployment, CI/CD, operations | Needed for validation deployment + CI gate |
| Performance | Profiling, bottlenecks | Not yet needed |
| Planner | Implementation decomposition | Used for Layer-1 build |
| Builder | General implementation | Manual-invoke; follow-ups awaiting invocation |
| Tester | Independent behavioral verification | Verified Layer-1; needed for coverage-gap closure (manual-invoke) |
| Reviewer | Independent review | Layer-1 reviewed (manual-invoke) |
| Debugger | Root-cause diagnosis | On-call (manual-invoke) |
| Documenter | Documentation accuracy | deployment.md fix in flight |
| Release Engineer | Release readiness | Not yet needed (no release cadence) |

## Skills

| Skill | Purpose | Owner |
|---|---|---|
| agent-engineering-workflow | Operating doctrine for the org | All agents; Captain primary |
| diagram-design | Architecture/diagram generation | Any agent when diagrams needed |
| customize-opencode | Editing opencode's own configuration | Only for opencode config work |

## Gaps / Missing Capabilities

- **UX/UI design specialist** — none; relevant when the frontend build starts (deferred)
- **Legal/compliance** — none; relevant at Layer 3 (escrow, payments, WHT)
- No missing engineering skill for the current phase; a recurring deployment/validation runbook skill may be worth codifying once the validation deployment is built and repeated
