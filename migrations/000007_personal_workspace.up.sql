-- ============================================================================
-- 000007_personal_workspace.up.sql
-- Personal default workspace: the organization auto-created at registration
-- becomes a PERSONAL workspace — no staff members may be added, invited,
-- re-role'd, or removed. Collaboration requires creating a NEW workspace.
-- Clients remain allowed on personal workspaces (provision/assign/approval
-- flows are client-scoped, not staff-scoped).
--
-- Backfill signal (composite, evidence-based):
--   * Naming: registration names the default org with
--     fmt.Sprintf("%s's Organization", req.FirstName) — the only code path
--     producing the "%s's Organization" pattern.
--   * Same-tx creation timestamp: PostgreSQL's NOW() returns the transaction
--     START time, so a user + org created in ONE transaction share identical
--     created_at values. Registration is the ONLY path that creates a user
--     and its default org in a single transaction; POST /v1/orgs creates an
--     org in a later transaction (created_at differs from the owner user's).
--   * Membership shape: exactly one non-client membership, and that member
--     holds the owner role. An invited (or otherwise added) staff membership
--     disqualifies the org — personal workspaces are single-owner by
--     definition. Client-role memberships are excluded from the staff count:
--     a personal org may already have provisioned clients.
--
-- golang-migrate wraps this migration in a transaction (atomic): the column,
-- the COMMENT, and the backfill either all apply or none do.
-- ============================================================================

ALTER TABLE organizations ADD COLUMN is_personal BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN organizations.is_personal IS
  'True = the registration-created personal workspace: no staff members may be added/removed/re-rolled. Clients are still allowed.';

-- Backfill (composite evidence-based rule):
UPDATE organizations o
SET is_personal = TRUE
WHERE o.is_personal = FALSE
  AND o.name LIKE '%''s Organization'          -- registration naming pattern
  AND o.created_at = (                        -- same-tx creation evidence: registration creates user+org in ONE tx,
        SELECT u.created_at                   -- so organizations.created_at == owner users.created_at (tx-start NOW())
        FROM memberships m
        JOIN roles r  ON r.id = m.role_id
        JOIN users u  ON u.id = m.user_id
        WHERE m.organization_id = o.id
          AND r.name <> 'client'
        LIMIT 1                               -- safety: rows with 2+ staff members would make the scalar
  )                                           -- subquery return multiple rows (planner-order dependent); the
                                              -- count(*)=1 conjunct below already excludes them, so LIMIT 1
                                              -- never changes which orgs qualify — it only prevents the
                                              -- "more than one row returned by a subquery" error.
  AND (SELECT count(*) FROM memberships m JOIN roles r ON r.id = m.role_id
       WHERE m.organization_id = o.id AND r.name <> 'client') = 1
  AND (SELECT count(*) FROM memberships m JOIN roles r ON r.id = m.role_id
       WHERE m.organization_id = o.id AND r.name = 'owner') = 1;
