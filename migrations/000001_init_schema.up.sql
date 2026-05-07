-- EXTENSIONS
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- USERS (global identity)
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    email           TEXT UNIQUE,
    phone           TEXT UNIQUE,

    password_hash   TEXT NOT NULL,

    first_name      TEXT,
    last_name       TEXT,

    status          TEXT NOT NULL DEFAULT 'active',
    -- active | suspended | deleted

    email_verified  BOOLEAN DEFAULT FALSE,
    phone_verified  BOOLEAN DEFAULT FALSE,

    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW(),

    CHECK (email IS NOT NULL OR phone IS NOT NULL)
);

-- ORGANIZATIONS (tenant root)
CREATE TABLE organizations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name            TEXT NOT NULL,
    slug            TEXT UNIQUE,

    status          TEXT NOT NULL DEFAULT 'active',
    -- active | suspended | deleted

    created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- MEMBERSHIPS (multi-tenancy bridge)
CREATE TABLE memberships (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,

    role            TEXT NOT NULL,
    -- owner | admin | member | viewer

    status          TEXT NOT NULL DEFAULT 'active',
    -- active | invited | suspended

    joined_at       TIMESTAMP DEFAULT NOW(),

    UNIQUE(user_id, organization_id)
);

-- SESSIONS (auth + device tracking)
CREATE TABLE sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    refresh_token_hash  TEXT UNIQUE NOT NULL,

    user_agent          TEXT,
    ip_address          TEXT,

    revoked             BOOLEAN DEFAULT FALSE,
    revoked_at          TIMESTAMP,

    expires_at          TIMESTAMP NOT NULL,
    created_at          TIMESTAMP NOT NULL DEFAULT NOW()
);

-- AUDIT LOGS (enterprise traceability)
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

    created_at      TIMESTAMP NOT NULL DEFAULT NOW()
);

-- INDEXES (performance-critical)
CREATE INDEX idx_memberships_user_org
ON memberships(user_id, organization_id);

CREATE INDEX idx_sessions_user_id
ON sessions(user_id);

CREATE INDEX idx_audit_logs_org_created
ON audit_logs(organization_id, created_at);

CREATE INDEX idx_users_email
ON users(email);

CREATE INDEX idx_users_phone
ON users(phone);
