# Egentop — Open Questions

> Unresolved questions, ambiguities, risks, and pending decisions. Remove or mark **RESOLVED** once decided.
> Last updated: 2026-08-14

---

## Q5 — Frontend decision — **SUPERSEDED 2026-08-14 (see DECISIONS.md)**

- **Resolution (2026-08-14):** ~~API-first validation before any frontend~~ — **superseded the same day.** The founder identified that agencies cannot operate a raw API and the client deep link returns JSON, not a rendered page. The minimal frontend is now approved and initialized in a separate codebase (`/home/miracle/projects/egentop-frontend`). Frontend build is the next major work stream.

---

## FE-O2 — Backend change: `revision_limit` on project payloads — **IMPLEMENTED 2026-08-15 (RESOLVED)**

- **Resolution:** Founder approved 2026-08-15; implemented by Builder in commit `42d0875`. Project list + detail now expose `revision_limit` for staff actors (stored value as-is, NULL omitted); client-role actors never see it (detail hides via handler role split; list is RBAC staff-only with a direct-handler test proving the exclusion — defense in depth). 136/136 tests (was 134). No further action.

---

## Q3 — Distribution channel

- **Question:** How do the first agencies find Egentop?
- **Why it matters:** The self-hosted/open-source playbook (HN, r/selfhosted) likely does NOT fit — this is a SaaS for non-technical agency owners. No distribution = no users regardless of product quality.
- **Current understanding:** Nothing established. HoneyBook used a large SEO/content engine; unknown what's viable here.
- **Possible approaches:** Agency partnerships, WhatsApp/social channels in target geography (if Nigeria), content/SEO later, freelancer communities.
- **Information needed:** Founder's answer; geography answer (Q1) feeds this.

---

## Q4 — Revenue / pricing model

- **Question:** What is the pricing shape? (per-seat, per-project, flat tiers, percentage?)
- **Why it matters:** "Agencies would pay" is asserted but unvalidated, and the model shapes product decisions (e.g., per-project pricing incentivizes the workflow depth).
- **Current understanding:** One-sentence hypothesis suggested: "self-hosted free, hosted SaaS paid, enterprise later" — but self-hosting is likely irrelevant for this audience; hypothesis unaccepted.
- **Possible approaches:** Flat tiers by seats; per-project; free trial → paid; freemium.
- **Information needed:** Founder decision; ideally early customer signals.

---

## Q5 — Frontend decision — **RESOLVED 2026-08-14**

- **Question:** Build a minimal frontend (timeboxed 8–12 weeks) or validate API-first with the agent angle?
- **Resolution (2026-08-14):** **API-first validation before any frontend.** No frontend build now; validate the wedge with 1–2 friendly agencies over the API (client approval deep link + one-time credentials). Revisit frontend only when validation signals justify it.
- **Why it matters:** Biggest time commitment on the horizon. For agencies, a client-facing approval portal is the actual product surface; agencies will tolerate rough internal tools but clients need dead-simple UX.
- **Current understanding:** No frontend exists. The Captain and Architect both lean toward a hard-scoped minimal frontend after Layer-1 delta; agent-first validation was the alternative if founder hours are tight.
- **Possible approaches:** (a) Minimal agency workspace + client approval portal; (b) agent-first API validation; (c) outsource frontend.
- **Information needed:** Founder's available hours/week (Q6) and wedge decision (Q2).

---

## Q6 — Founder availability — **RESOLVED 2026-08-14**

- **Question:** How many hours per week can the founder actually spend?
- **Resolution (2026-08-14):** Available every day, on-call via the Captain session. Engineering capacity is agent-assisted; founder hours go to agency contact and decisions.
- **Why it matters:** Calibrates whether the 90-day plan is solo, solo + agent-assisted, or needs a hire/outsourcing.
- **Current understanding:** Unknown.
- **Information needed:** Founder statement.

---

## Q7 — Escrow trust asymmetry (Layer 3 risk)

- **Question:** How does the *client* (not just the agency) come to trust Egentop with money?
- **Why it matters:** Escrow is three-party; the vision's trust narrative is agency-side only. Proceeding to escrow without client-side trust would repeat the original mistake at a later stage.
- **Current understanding:** Proposed mitigation: Layer 3 starts with agency-facing invoicing + payment tracking (no client trust needed); escrow last, only after clients have interacted with the system.
- **Possible approaches:** Adopt the proposed sequence; or design a client-trust program (deposits, refunds, dispute handling) before escrow.
- **Information needed:** Founder acceptance of the proposed Layer-3 sequence (currently Proposed, not committed).

---

## Q8 — AI layer timing

- **Question:** When does the AI layer actually begin, and what triggers it?
- **Why it matters:** AI is confirmed as NOT the MVP, but it's the moat and the name ("agent-top") carries the ambition. The product's data model must stay AI-ready (event-rich) from now.
- **Current understanding:** Confirmed sequencing (after workflow data); the audit/activity substrate is compatible. No trigger defined (user count? revenue? stage?).
- **Possible approaches:** Define a concrete trigger (e.g., N active agencies, Layer-2 completion); begin with one narrow AI feature (scope analyzer or risk detection) rather than the full suite.
- **Information needed:** Founder's view on trigger + which first AI feature.

---

## Q9 — Legacy documentation reconciliation — **RESOLVED 2026-08-14**

- **Question:** What happens to `docs/roadmap.md` (generic-PM framing, conflicts with the four-layer vision)?
- **Resolution (2026-08-14):** `docs/roadmap.md` was rewritten to the four-layer vision + API-first validation framing and now declares `.captain/ROADMAP.md` canonical at top and bottom. README framing also corrected. Done as part of the Documenter verification pass (`f3d0230`).
- **Why it mattered:** Two roadmaps with different directions = contradiction; future sessions may read the wrong one.

---

## Q10 — Email provider choice

- **Question:** Which email delivery provider? (Resend / SES / Postmark / others)
- **Why it matters:** Transactional email (invites, reset) is Layer-1 infrastructure; provider choice affects cost, deliverability, and geography (SES needs AWS; Resend is simple).
- **Current understanding:** "Buy, not build" is agreed in principle (proposed); no provider chosen.
- **Possible approaches:** Resend (simple, good DX), AWS SES (cheap, needs AWS), Postmark (deliverability-focused).
- **Information needed:** Deployment target (deploy-time) and geography (Q1).

---

## Q11 — Client role design — **RESOLVED 2026-08-14**

- **Question:** What exactly does a `client` membership/role see and do? (scope of permissions, project visibility rules, approval rights)
- **Resolution:** Founder approved Option A (real `users` row + `memberships` row + new `client` system role; `projects.client_id`; service-layer project-scope enforcement; never a separate clients table) and both sub-questions:
  - **Sub-question 1 (provisioning):** provision user with a **one-time credential returned in the invite response**, shared out-of-band by the agency (WhatsApp fits the market). No email provider for MVP; `password_hash NOT NULL` handled via provisioning. Email delivery slots in later.
  - **Sub-question 2 (visibility):** clients are **project-scoped only**. Narrow keys: `project.view`, `milestone.view`, `milestone.approve`, `milestone.revision.request`. Never `member.list`, `activity.list`, `org.*`. Clients get project-scoped activity only.
- **Consequences:** RBAC gains a seeded `client` system template role (seed-only change). Guard `member.role.update` against escalating client memberships. The tenant-isolation fix (2026-08-13) is the prerequisite and is complete.

---

## RESOLVED

- **Q: Is "Egentop" = "agent top"?** — RESOLVED 2026-08-13: Yes, intentional; the agent/intelligence layer is the top layer. The name stays; AI is Layer 4, not the MVP.
- **Q: Should we build escrow first?** — RESOLVED 2026-08-13: No. Workflow-first; escrow becomes a feature. (Recorded in DECISIONS.md.)
- **Q: Build the AI agent product now?** — RESOLVED 2026-08-13: No. AI comes after workflow data exists; it is not the MVP.
- **Q1 — Target geography / market** — RESOLVED 2026-08-14: **Nigeria/West Africa confirmed by founder.** Paystack/Flutterwave rails, WhatsApp client channel, Nigerian WHT (5% services / 10% consultancy) requirements apply. Layer-3 provider class: Paystack/Flutterwave.
- **Q2 — Competitive wedge** — RESOLVED 2026-08-14: **Reframed wedge approved by founder.** Wedge = milestone-level sign-off → revision limits → invoicing → payment status for 2–20 person agencies, especially with African payment rails. Layer-1 is designed around this.
- **Q12 — Revision-limit semantics** — RESOLVED 2026-08-14: **Track + flag, no hard cap.** `revision_count` + `milestone_revisions` history + configurable per-project/milestone limit; `limit_reached` flag surfaces over-revision to the agency without blocking the client approval path.
- **Q13 — Payment status scope** — RESOLVED 2026-08-14: **Per-milestone status** (unpaid/partial/paid), agency-updated, display-only, no money movement; visible on the client approval view.
- **Q5 — Frontend decision** — SUPERSEDED 2026-08-14 (see DECISIONS.md): the API-first-only stance was replaced the same day by "minimal frontend now, build before validation" — agencies cannot operate a raw API.
- **Q6 — Founder availability** — RESOLVED 2026-08-14: available every day / on-call via the Captain session; engineering capacity is agent-assisted.