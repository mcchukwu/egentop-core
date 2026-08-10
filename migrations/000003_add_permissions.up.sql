-- Add permissions for project/milestone metadata updates and activity feed
INSERT INTO permissions (key, description) VALUES
    ('project.update',    'Update a project''s details'),
    ('milestone.update',  'Update a milestone''s details'),
    ('activity.list',     'List organization activity')
ON CONFLICT (key) DO NOTHING;

-- ADMIN: project/milestone updates
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key IN (
    'project.update',
    'milestone.update'
)
WHERE r.name = 'admin'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- MEMBER: read the activity feed
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key = 'activity.list'
WHERE r.name = 'member'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- VIEWER: read the activity feed
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
JOIN permissions p ON p.key = 'activity.list'
WHERE r.name = 'viewer'
  AND r.organization_id IS NULL
ON CONFLICT (role_id, permission_id) DO NOTHING;
