DROP INDEX IF EXISTS idx_sessions_token_lookup_hash;
ALTER TABLE sessions DROP COLUMN IF EXISTS token_lookup_hash;
