-- PROJECT DOMAIN
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL
        REFERENCES organizations(id)
        ON DELETE CASCADE,

    created_by UUID NOT NULL
        REFERENCES users(id)
        ON DELETE RESTRICT,

    name TEXT NOT NULL,

    description TEXT,

    status TEXT NOT NULL DEFAULT 'draft',

    priority TEXT NOT NULL DEFAULT 'medium',

    due_date TIMESTAMP,

    archived_at TIMESTAMP,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    CONSTRAINT projects_name_length
        CHECK (char_length(name) >= 1),

    CONSTRAINT projects_status_check
        CHECK (
            status IN (
                'draft',
                'active',
                'completed',
                'archived',
                'cancelled'
            )
        ),

    CONSTRAINT projects_priority_check
        CHECK (
            priority IN (
                'low',
                'medium',
                'high'
            )
        )
);

-- PROJECT TRIGGERS
CREATE TRIGGER set_projects_updated_at
BEFORE UPDATE ON projects
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- PROJECT INDEXES
CREATE INDEX idx_projects_organization_id
ON projects(organization_id);

CREATE INDEX idx_projects_created_by
ON projects(created_by);

CREATE INDEX idx_projects_status
ON projects(status);

CREATE INDEX idx_projects_priority
ON projects(priority);

CREATE INDEX idx_projects_due_date
ON projects(due_date);

CREATE INDEX idx_projects_created_at
ON projects(created_at DESC);

CREATE INDEX idx_projects_org_status
ON projects(organization_id, status);

CREATE INDEX idx_projects_org_created_at
ON projects(organization_id, created_at DESC);

-- MILESTONES
CREATE TABLE milestones (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL
        REFERENCES organizations(id)
        ON DELETE CASCADE,

    project_id UUID NOT NULL
        REFERENCES projects(id)
        ON DELETE CASCADE,

    created_by UUID NOT NULL
        REFERENCES users(id)
        ON DELETE RESTRICT,

    title TEXT NOT NULL,

    description TEXT,

    status TEXT NOT NULL DEFAULT 'pending',

    due_date TIMESTAMP,

    position INTEGER NOT NULL DEFAULT 0,

    completed_at TIMESTAMP,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    CONSTRAINT milestones_title_length
        CHECK (char_length(title) >= 1),

    CONSTRAINT milestones_position_check
        CHECK (position >= 0),

    CONSTRAINT milestones_status_check
        CHECK (
            status IN (
                'pending',
                'in_progress',
                'awaiting_approval',
                'completed',
                'blocked',
                'cancelled'
            )
        )
);

-- MILESTONE TRIGGERS
CREATE TRIGGER set_milestones_updated_at
BEFORE UPDATE ON milestones
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();

-- MILESTONE INDEXES
CREATE INDEX idx_milestones_organization_id
ON milestones(organization_id);

CREATE INDEX idx_milestones_project_id
ON milestones(project_id);

CREATE INDEX idx_milestones_created_by
ON milestones(created_by);

CREATE INDEX idx_milestones_status
ON milestones(status);

CREATE INDEX idx_milestones_due_date
ON milestones(due_date);

CREATE INDEX idx_milestones_position
ON milestones(project_id, position);

CREATE INDEX idx_milestones_project_status
ON milestones(project_id, status);

CREATE INDEX idx_milestones_created_at
ON milestones(created_at DESC);

-- ASSIGNMENTS
CREATE TABLE assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL
        REFERENCES organizations(id)
        ON DELETE CASCADE,

    project_id UUID
        REFERENCES projects(id)
        ON DELETE CASCADE,

    milestone_id UUID
        REFERENCES milestones(id)
        ON DELETE CASCADE,

    assigned_to UUID NOT NULL
        REFERENCES users(id)
        ON DELETE CASCADE,

    assigned_by UUID NOT NULL
        REFERENCES users(id)
        ON DELETE RESTRICT,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    CONSTRAINT assignments_target_check
        CHECK (
            project_id IS NOT NULL
            OR milestone_id IS NOT NULL
        )
);

-- ASSIGNMENT INDEXES
CREATE INDEX idx_assignments_organization_id
ON assignments(organization_id);

CREATE INDEX idx_assignments_project_id
ON assignments(project_id);

CREATE INDEX idx_assignments_milestone_id
ON assignments(milestone_id);

CREATE INDEX idx_assignments_assigned_to
ON assignments(assigned_to);

CREATE INDEX idx_assignments_assigned_by
ON assignments(assigned_by);

CREATE INDEX idx_assignments_created_at
ON assignments(created_at DESC);

CREATE INDEX idx_assignments_org_assigned_to
ON assignments(organization_id, assigned_to);

-- ACTIVITIES
CREATE TABLE activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    organization_id UUID NOT NULL
        REFERENCES organizations(id)
        ON DELETE CASCADE,

    project_id UUID
        REFERENCES projects(id)
        ON DELETE CASCADE,

    milestone_id UUID
        REFERENCES milestones(id)
        ON DELETE CASCADE,

    actor_id UUID
        REFERENCES users(id)
        ON DELETE SET NULL,

    type TEXT NOT NULL,

    message TEXT NOT NULL,

    metadata JSONB,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    CONSTRAINT activities_type_length
        CHECK (char_length(type) >= 1),

    CONSTRAINT activities_message_length
        CHECK (char_length(message) >= 1)
);

-- ACTIVITY INDEXES
CREATE INDEX idx_activities_organization_id
ON activities(organization_id);

CREATE INDEX idx_activities_project_id
ON activities(project_id);

CREATE INDEX idx_activities_milestone_id
ON activities(milestone_id);

CREATE INDEX idx_activities_actor_id
ON activities(actor_id);

CREATE INDEX idx_activities_type
ON activities(type);

CREATE INDEX idx_activities_created_at
ON activities(created_at DESC);

CREATE INDEX idx_activities_project_created_at
ON activities(project_id, created_at DESC);

CREATE INDEX idx_activities_org_created_at
ON activities(organization_id, created_at DESC);
