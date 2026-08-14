-- ============================================================================
-- 000005_layer1_delta.up.sql
-- Layer-1 delta: client role, milestone approval state machine, revision
-- tracking, link-based deliverables, per-milestone payment status, forced
-- password change, AI-readiness audit index, and RBAC seed reconciliation.
--
-- Design notes:
--  * milestone_status is extended via a FULL COLUMN REWRITE (enum -> TEXT ->
--    new enum) in BOTH directions. ALTER TYPE ... ADD VALUE is avoided: it is
--    not reversible and its new values cannot be used inside the applying
--    transaction. The rewrite also makes the down migration able to REMOVE
--    the new values.
--  * milestone_status is duplicated in the enum AND in the
--    milestones_status_check CHECK constraint (known project trap). Both lists
--    below must stay in sync.
--  * All seed statements use ON CONFLICT ... DO NOTHING and are idempotent.
--  * Applied atomically by `make migrate-up` (golang-migrate wraps each
--    migration in a transaction; PostgreSQL DDL is transactional).
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1. milestone_status: extend to 'approved' and 'changes_requested'
-- ---------------------------------------------------------------------------
-- Drop the CHECK first: it lists the old values, cannot be auto-cast through
-- the type change, and would reject the new statuses.
ALTER TABLE milestones DROP CONSTRAINT IF EXISTS milestones_status_check;

-- The default ('pending'::milestone_status) is a dependency of the enum type;
-- drop it before the type is replaced.
ALTER TABLE milestones ALTER COLUMN status DROP DEFAULT;

-- Step 1: widen to TEXT. Every existing value is a valid enum label, so this
-- cast is lossless. (idx_milestones_status is rebuilt automatically.)
ALTER TABLE milestones ALTER COLUMN status TYPE TEXT USING status::text;

-- Step 2: replace the now dependency-free enum with the extended one.
DROP TYPE IF EXISTS milestone_status;
CREATE TYPE milestone_status AS ENUM (
    'pending', 'in_progress', 'awaiting_approval',
    'completed', 'blocked', 'cancelled',
    'approved', 'changes_requested'
);

-- Step 3: narrow back to the enum. Existing rows only use labels present in
-- the new enum, so this cast is lossless.
ALTER TABLE milestones ALTER COLUMN status TYPE milestone_status USING status::milestone_status;

-- Restore the original default and re-establish the CHECK.
ALTER TABLE milestones ALTER COLUMN status SET DEFAULT 'pending';
ALTER TABLE milestones ADD CONSTRAINT milestones_status_check CHECK (
    status IN (
        'pending', 'in_progress', 'awaiting_approval',
        'completed', 'blocked', 'cancelled',
        'approved', 'changes_requested'
    )
);

-- ---------------------------------------------------------------------------
-- 2. Milestone payment status (display-only, agency-updated)
-- ---------------------------------------------------------------------------
CREATE TYPE milestone_payment_status AS ENUM ('unpaid', 'partial', 'paid');

ALTER TABLE milestones
    ADD COLUMN payment_status milestone_payment_status NOT NULL DEFAULT 'unpaid';

-- NOTE: enum + CHECK duplication trap, same as milestone_status.
ALTER TABLE milestones ADD CONSTRAINT milestones_payment_status_check CHECK (
    payment_status IN ('unpaid', 'partial', 'paid')
);

-- ---------------------------------------------------------------------------
-- 3. Revision tracking
--    revision_count: submission rounds (0 before first submission).
--    revision_limit: NULL = no limit (COALESCE(milestone.revision_limit,
--    project.revision_limit) treats NULL as unlimited). An explicit limit of 0
--    is forbidden: revision_count starts at 0, so `0 >= 0` would make
--    limit_reached permanently true before the first submission.
-- ---------------------------------------------------------------------------
ALTER TABLE milestones
    ADD COLUMN revision_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN revision_limit  INTEGER;

ALTER TABLE milestones ADD CONSTRAINT milestones_revision_count_check
    CHECK (revision_count >= 0);
ALTER TABLE milestones ADD CONSTRAINT milestones_revision_limit_check
    CHECK (revision_limit IS NULL OR revision_limit >= 1);

ALTER TABLE projects
    ADD COLUMN revision_limit INTEGER;

ALTER TABLE projects ADD CONSTRAINT projects_revision_limit_check
    CHECK (revision_limit IS NULL OR revision_limit >= 1);

-- ---------------------------------------------------------------------------
-- 4. projects.client_id (single client per project for MVP; join table later)
--    ON DELETE SET NULL: a project is valid without a client, and user
--    deletion must neither orphan nor be blocked by a project reference.
--    Explicit assign/reassign/unassign is owned by the service
--    (project.client.assign).
-- ---------------------------------------------------------------------------
ALTER TABLE projects
    ADD COLUMN client_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX idx_projects_org_client ON projects(organization_id, client_id);

-- ---------------------------------------------------------------------------
-- 5. users.must_change_password (forced change on first login; one-time
--    credentials must be rotated before the client can act)
-- ---------------------------------------------------------------------------
ALTER TABLE users
    ADD COLUMN must_change_password BOOLEAN NOT NULL DEFAULT FALSE;

-- ---------------------------------------------------------------------------
-- 6. milestone_revisions — submission history (one row per submission round)
--    Append-only: no updated_at column, no trigger (consistent with
--    audit_logs). submitted_by is RESTRICT, matching the created_by /
--    assigned_by identity-preservation convention elsewhere.
-- ---------------------------------------------------------------------------
CREATE TABLE milestone_revisions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    milestone_id     UUID NOT NULL REFERENCES milestones(id) ON DELETE CASCADE,
    revision_number  INTEGER NOT NULL,
    submitted_by     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    submitted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes            TEXT,

    CONSTRAINT milestone_revisions_revision_number_check
        CHECK (revision_number >= 1),
    CONSTRAINT milestone_revisions_milestone_revision_unique
        UNIQUE (milestone_id, revision_number)
);

CREATE INDEX idx_milestone_revisions_org_milestone
    ON milestone_revisions(organization_id, milestone_id);

-- ---------------------------------------------------------------------------
-- 7. milestone_deliverables — link-based deliverables, no storage infra.
--    Deliberately NOT project-scoped: deliverables are only ever reached
--    through a milestone, and the service validates milestone ownership
--    (milestone -> project -> organization) before any read/write; the
--    organization_id column preserves query-level tenant isolation. Adding a
--    denormalized project_id would require app-maintained consistency for no
--    query benefit.
-- ---------------------------------------------------------------------------
CREATE TABLE milestone_deliverables (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id  UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    milestone_id     UUID NOT NULL REFERENCES milestones(id) ON DELETE CASCADE,
    url              TEXT NOT NULL,
    title            TEXT,
    description      TEXT,
    submitted_by     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    submitted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT milestone_deliverables_url_prefix_check
        CHECK (url LIKE 'http://%' OR url LIKE 'https://%'),
    CONSTRAINT milestone_deliverables_title_length_check
        CHECK (title IS NULL OR char_length(title) <= 200),
    CONSTRAINT milestone_deliverables_description_length_check
        CHECK (description IS NULL OR char_length(description) <= 2000)
);

CREATE INDEX idx_milestone_deliverables_org_milestone
    ON milestone_deliverables(organization_id, milestone_id);

CREATE INDEX idx_milestone_deliverables_milestone_submitted
    ON milestone_deliverables(milestone_id, submitted_at DESC);

-- ---------------------------------------------------------------------------
-- 8. Audit index for per-entity history (AI-readiness)
-- ---------------------------------------------------------------------------
CREATE INDEX idx_audit_logs_entity_created
    ON audit_logs(entity_type, entity_id, created_at DESC);

-- ===========================================================================
-- 9. SEEDS — permissions, client role, grants
-- ===========================================================================

-- 9.1 New permission keys (9).
-- NOTE: milestone.approve and milestone.revision.request were missing from the
-- Architect sketch's key list but are required by the client-role grants.
INSERT INTO permissions (key, description) VALUES
    ('client.provision',                'Provision a client account with a one-time credential'),
    ('client.list',                     'List clients in the organization'),
    ('project.client.assign',           'Assign or unassign a client to a project'),
    ('milestone.submit',                'Submit a milestone for client approval'),
    ('deliverable.submit',              'Submit a deliverable on a milestone'),
    ('milestone.approve',               'Approve a submitted milestone'),
    ('milestone.revision.request',      'Request changes on a submitted milestone'),
    ('milestone.payment_status.update', 'Update a milestone''s payment status'),
    ('activity.project.list',           'List project-scoped activity')
ON CONFLICT (key) DO NOTHING;

-- 9.2 New system template role: client (is_system protects it from deletion).
INSERT INTO roles (organization_id, name, is_system) VALUES
    (NULL, 'client', TRUE)
ON CONFLICT DO NOTHING;

-- 9.3 OWNER: every permission in the system.
-- Re-running the 000001 CROSS JOIN is idempotent (ON CONFLICT DO NOTHING) and
-- closes the 000003 gap: owner was never granted project.update,
-- milestone.update, or activity.list.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'owner'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- 9.4 ADMIN: client management, payment status, project-scoped activity, the
-- staff delivery workflow (milestone.submit / deliverable.submit preserve the
-- documented admin >= member hierarchy in docs/api.md), plus the 000003 gap
-- fix: activity.list (000003 granted it only to member/viewer, but
-- docs/api.md documents it for admin).
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'activity.list',
    'client.provision', 'client.list', 'project.client.assign',
    'milestone.submit', 'deliverable.submit',
    'milestone.payment_status.update',
    'activity.project.list'
)
WHERE r.name = 'admin'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- 9.5 MEMBER: staff delivery workflow + project-scoped activity.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'milestone.submit', 'deliverable.submit', 'activity.project.list'
)
WHERE r.name = 'member'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- 9.6 VIEWER: project-scoped activity (read-only).
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key = 'activity.project.list'
WHERE r.name = 'viewer'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- 9.7 CLIENT: narrow, project-scoped read + approval surface only.
-- Never granted list/org/member keys. Service layer additionally enforces
-- project scope (requestctx.Role) and member.list must exclude client members.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'project.view',
    'milestone.view',
    'milestone.approve',
    'milestone.revision.request',
    'activity.project.list'
)
WHERE r.name = 'client'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;
