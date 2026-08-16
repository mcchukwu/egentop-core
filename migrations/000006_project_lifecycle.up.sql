-- ============================================================================
-- 000006_project_lifecycle.up.sql
-- Project lifecycle slice: soft-delete support for projects.
--
-- Design notes:
--  * deleted_at NULL = live. A non-NULL value = soft-deleted: the row is
--    preserved so the audit trail and the activity feed keep the project's
--    history (activities.project_id is ON DELETE CASCADE, so a hard delete
--    would erase that history).
--  * Restore is logic-only: project_status already carries 'active', and
--    activities.type / audit_logs.action are TEXT, so the new event strings
--    (project.deleted / project.restored) need no schema change.
--  * NO new index: the existing idx_projects_org_created_at (000002) serves
--    the default list. An optional partial index (deleted_at IS NULL) is a
--    documented future option only.
-- ============================================================================

ALTER TABLE projects ADD COLUMN deleted_at TIMESTAMPTZ;
