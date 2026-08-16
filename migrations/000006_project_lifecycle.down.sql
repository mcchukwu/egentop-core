-- ============================================================================
-- 000006_project_lifecycle.down.sql
-- Drops the soft-delete column. NOTE: dropping the column re-exposes
-- soft-deleted rows as live (deleted_at becomes NULL for every row). No
-- data-preserving transform exists — the deletion timestamps are gone with
-- the column. This is inherent to the reversible change, consistent with the
-- 000005 down treatment of dropped columns.
-- ============================================================================

ALTER TABLE projects DROP COLUMN IF EXISTS deleted_at;
