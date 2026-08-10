-- EXTENSIONS
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- SHARED FUNCTIONS
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- USERS (global identity)
CREATE TYPE user_status AS ENUM ('active', 'suspended', 'deleted');

CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    email           TEXT UNIQUE,
    phone           TEXT UNIQUE,

    password_hash   TEXT NOT NULL,

    first_name      TEXT NOT NULL,
    last_name       TEXT NOT NULL,

    status          user_status NOT NULL DEFAULT 'active',

    email_verified  BOOLEAN NOT NULL DEFAULT FALSE,
    phone_verified  BOOLEAN NOT NULL DEFAULT FALSE,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (email IS NOT NULL OR phone IS NOT NULL)
);

CREATE TRIGGER users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_phone ON users(phone);

-- ORGANIZATIONS (tenants)
CREATE TYPE organization_status AS ENUM ('active', 'suspended', 'deleted');

CREATE TABLE organizations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name            TEXT NOT NULL,
    slug            TEXT UNIQUE,

    status          organization_status NOT NULL DEFAULT 'active',

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER organizations_updated_at
BEFORE UPDATE ON organizations
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- PERMISSIONS (the atomic, checkable actions in the system)
CREATE TABLE permissions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    key         TEXT UNIQUE NOT NULL,
    -- e.g. 'project.delete', 'invoice.approve', 'member.invite'

    description TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ROLES (data-driven, scoped per organization)
CREATE TABLE roles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID REFERENCES organizations(id) ON DELETE CASCADE,
    -- NULL organization_id = system-wide template role (seeded defaults)

    name            TEXT NOT NULL,

    is_system       BOOLEAN NOT NULL DEFAULT FALSE,
    -- TRUE = seeded default (owner/admin/member/viewer), protected from deletion

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(organization_id, name)
);

-- system template roles must be unique by name even though organization_id is NULL
-- (plain UNIQUE treats NULLs as distinct, which would allow duplicate system roles)
CREATE UNIQUE INDEX idx_roles_system_name
ON roles(name)
WHERE organization_id IS NULL;

CREATE TRIGGER roles_updated_at
BEFORE UPDATE ON roles
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

CREATE INDEX idx_roles_organization_id ON roles(organization_id);

-- ROLE PERMISSIONS (which permissions a role grants)
CREATE TABLE role_permissions (
    role_id         UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id   UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,

    granted_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX idx_role_permissions_permission_id ON role_permissions(permission_id);

-- MEMBERSHIPS (multi-tenancy bridge — also points at a role)
CREATE TYPE membership_status AS ENUM ('active', 'invited', 'suspended');

CREATE TABLE memberships (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role_id         UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    -- RESTRICT: can't delete a role while members still hold it

    status          membership_status NOT NULL DEFAULT 'active',

    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE(user_id, organization_id)
);

CREATE TRIGGER memberships_updated_at
BEFORE UPDATE ON memberships
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- single index covers both the lookup and uniqueness — no redundant duplicate
CREATE UNIQUE INDEX idx_unique_membership ON memberships(user_id, organization_id);
CREATE INDEX idx_memberships_role_id ON memberships(role_id);

-- SESSIONS (auth + device tracking, with refresh token rotation support)
CREATE TABLE sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    token_family_id     UUID NOT NULL DEFAULT gen_random_uuid(),
    -- shared across all rotated tokens in one continuous session lineage;
    -- reuse of a revoked token => revoke entire family (theft signal)

    refresh_token_hash  TEXT UNIQUE NOT NULL,

    user_agent          TEXT,
    ip_address          TEXT,

    revoked             BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at          TIMESTAMPTZ,

    expires_at          TIMESTAMPTZ NOT NULL,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_token_family_id ON sessions(token_family_id);

-- AUDIT LOGS (business events — what successfully happened)
CREATE TABLE audit_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    user_id         UUID REFERENCES users(id) ON DELETE SET NULL,

    action          TEXT NOT NULL,
    -- e.g. "user.created", "membership.role_changed"

    entity_type     TEXT,
    entity_id       UUID,

    metadata        JSONB,

    ip_address      TEXT,
    user_agent      TEXT,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_org_created ON audit_logs(organization_id, created_at);

-- AUTHZ DECISIONS (every authorization check — allowed or denied)
CREATE TABLE authz_decisions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID REFERENCES organizations(id) ON DELETE SET NULL,
    user_id         UUID REFERENCES users(id) ON DELETE SET NULL,

    permission_key  TEXT NOT NULL,
    -- denormalized snapshot, not a FK — so the log still reads correctly
    -- even if the permission is later renamed or deleted

    resource_type   TEXT,
    resource_id     UUID,

    allowed         BOOLEAN NOT NULL,
    reason          TEXT,
    -- e.g. "role lacks permission", "resource outside tenant scope"

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_authz_decisions_org_created ON authz_decisions(organization_id, created_at);
CREATE INDEX idx_authz_decisions_user_id ON authz_decisions(user_id);

-- ===========================================================================
-- SEED DATA — system permissions, roles and role->permission mappings
-- ===========================================================================

INSERT INTO permissions (key, description) VALUES
    ('member.list',           'List organization members'),
    ('member.invite',         'Add a member to an organization'),
    ('member.role.update',    'Change a member''s role'),
    ('member.remove',         'Remove a member from an organization'),

    ('project.create',        'Create a project'),
    ('project.list',          'List projects'),
    ('project.view',          'View a project''s details'),
    ('project.status.update', 'Update a project''s status'),

    ('milestone.create',        'Create a milestone'),
    ('milestone.list',          'List milestones'),
    ('milestone.view',          'View a milestone''s details'),
    ('milestone.status.update', 'Update a milestone''s status'),

    ('assignment.create', 'Create an assignment'),
    ('assignment.list',   'List assignments'),
    ('assignment.view',   'View an assignment'),
    ('assignment.update', 'Update an assignment'),
    ('assignment.remove', 'Remove an assignment'),

    ('org.view',   'View organization details'),
    ('org.update', 'Update organization details')
ON CONFLICT (key) DO NOTHING;

-- system template roles (organization_id IS NULL)
INSERT INTO roles (organization_id, name, is_system) VALUES
    (NULL, 'owner',  TRUE),
    (NULL, 'admin',  TRUE),
    (NULL, 'member', TRUE),
    (NULL, 'viewer', TRUE)
ON CONFLICT DO NOTHING;

-- OWNER: every permission
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'owner'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- ADMIN: manage members, projects, milestones and assignments (but not
-- owner-only actions such as member.role.update / member.remove)
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'member.list', 'member.invite',
    'project.create', 'project.list', 'project.view', 'project.status.update',
    'milestone.create', 'milestone.list', 'milestone.view', 'milestone.status.update',
    'assignment.create', 'assignment.list', 'assignment.view', 'assignment.update', 'assignment.remove',
    'org.view', 'org.update'
)
WHERE r.name = 'admin'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- MEMBER: read and list project/milestone/assignment data
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'project.list', 'project.view',
    'milestone.list', 'milestone.view',
    'assignment.list', 'assignment.view',
    'org.view'
)
WHERE r.name = 'member'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- VIEWER: read-only, same scope as member for now
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'project.list', 'project.view',
    'milestone.list', 'milestone.view',
    'assignment.list', 'assignment.view',
    'org.view'
)
WHERE r.name = 'viewer'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;
