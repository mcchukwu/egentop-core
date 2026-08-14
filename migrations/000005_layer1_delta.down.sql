-- ============================================================================
-- 000005_layer1_delta.down.sql
-- Reverses 000005, restoring the exact pre-000005 state.
-- Data-preserving: 'approved' -> 'completed', 'changes_requested' ->
-- 'awaiting_approval' inside the status rewrite.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1. Remove Layer-1 seeds
-- ---------------------------------------------------------------------------

-- 1.1 Remove every role_permissions row that 000005 created:
--     (a) all 9 new Layer-1 keys, from every role;
--     (b) the bundled 000003 gap-fix grants (owner: project.update /
--         milestone.update / activity.list; admin: activity.list), so the RBAC
--         state is exactly what it was before 000005.
--     Verified: no such rows existed pre-000005, so nothing else is deleted.
DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE key IN (
        'client.provision', 'client.list', 'project.client.assign',
        'milestone.submit', 'deliverable.submit',
        'milestone.approve', 'milestone.revision.request',
        'milestone.payment_status.update', 'activity.project.list'
    )
)
OR (
    role_id IN (SELECT id FROM roles WHERE name = 'owner' AND organization_id IS NULL)
    AND permission_id IN (
        SELECT id FROM permissions
        WHERE key IN ('project.update', 'milestone.update', 'activity.list')
    )
)
OR (
    role_id IN (SELECT id FROM roles WHERE name = 'admin' AND organization_id IS NULL)
    AND permission_id IN (SELECT id FROM permissions WHERE key = 'activity.list')
);

-- 1.2 Delete the client system role. Its remaining role_permissions rows
--     (on pre-existing keys project.view / milestone.view) cascade via
--     role_permissions.role_id ON DELETE CASCADE.
--     NOTE: FAILS if any memberships row still references the client role
--     (memberships.role_id is ON DELETE RESTRICT). Roll back before any
--     client provisioning, or remove those memberships first.
DELETE FROM roles WHERE name = 'client' AND organization_id IS NULL;

-- 1.3 Delete the new permission keys (role_permissions cascade is redundant
--     here but harmless).
DELETE FROM permissions WHERE key IN (
    'client.provision', 'client.list', 'project.client.assign',
    'milestone.submit', 'deliverable.submit',
    'milestone.approve', 'milestone.revision.request',
    'milestone.payment_status.update', 'activity.project.list'
);

-- ---------------------------------------------------------------------------
-- 2. Drop new tables (children first)
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS milestone_deliverables;
DROP TABLE IF EXISTS milestone_revisions;

-- ---------------------------------------------------------------------------
-- 3. Drop new indexes, columns, and the payment enum
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_projects_org_client;
DROP INDEX IF EXISTS idx_audit_logs_entity_created;

ALTER TABLE milestones
    DROP COLUMN IF EXISTS payment_status,
    DROP COLUMN IF EXISTS revision_count,
    DROP COLUMN IF EXISTS revision_limit;

ALTER TABLE projects
    DROP COLUMN IF EXISTS revision_limit,
    DROP COLUMN IF EXISTS client_id;

ALTER TABLE users
    DROP COLUMN IF EXISTS must_change_password;

DROP TYPE IF EXISTS milestone_payment_status;

-- ---------------------------------------------------------------------------
-- 4. milestone_status: restore the 6-value enum, data-preserving
--    'approved'          -> 'completed'
--    'changes_requested' -> 'awaiting_approval'
-- ---------------------------------------------------------------------------
ALTER TABLE milestones DROP CONSTRAINT IF EXISTS milestones_status_check;
ALTER TABLE milestones ALTER COLUMN status DROP DEFAULT;

ALTER TABLE milestones ALTER COLUMN status TYPE TEXT USING status::text;

-- Map the Layer-1 statuses to pre-existing labels while the column is TEXT
-- (otherwise the cast back to the 6-value enum would fail).
UPDATE milestones
SET status = CASE status
    WHEN 'approved' THEN 'completed'
    WHEN 'changes_requested' THEN 'awaiting_approval'
    ELSE status
END
WHERE status IN ('approved', 'changes_requested');

DROP TYPE IF EXISTS milestone_status;
CREATE TYPE milestone_status AS ENUM (
    'pending', 'in_progress', 'awaiting_approval',
    'completed', 'blocked', 'cancelled'
);

ALTER TABLE milestones ALTER COLUMN status TYPE milestone_status USING status::milestone_status;
ALTER TABLE milestones ALTER COLUMN status SET DEFAULT 'pending';
ALTER TABLE milestones ADD CONSTRAINT milestones_status_check CHECK (
    status IN ('pending', 'in_progress', 'awaiting_approval', 'completed', 'blocked', 'cancelled')
);
