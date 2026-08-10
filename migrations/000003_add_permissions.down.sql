DELETE FROM role_permissions
WHERE permission_id IN (
    SELECT id FROM permissions WHERE key IN ('project.update', 'milestone.update', 'activity.list')
);

DELETE FROM permissions
WHERE key IN ('project.update', 'milestone.update', 'activity.list');
