-- ============================================================================
-- 000007_personal_workspace.down.sql
-- Drops the is_personal column. NOTE: dropping the column loses the marker
-- data — every org reverts to workspace behavior (staff members become
-- addable/invitable/re-roleable/removable again, including the previously
-- personal defaults). No data-preserving transform exists; this is inherent
-- to the reversible change, consistent with the 000006 down treatment of
-- dropped columns.
-- ============================================================================

ALTER TABLE organizations DROP COLUMN IF EXISTS is_personal;
