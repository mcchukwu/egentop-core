-- SESSIONS: deterministic lookup fingerprint for refresh-token rotation.
--
-- refresh_token_hash is a salted bcrypt hash (non-deterministic), so a session
-- cannot be looked up by raw cookie value. token_lookup_hash is a SHA-256 hex
-- fingerprint of the raw refresh token that enables fast, indexed lookups by
-- cookie value. The bcrypt hash remains the authoritative stored secret; the
-- fingerprint only locates candidate rows, which are then verified with bcrypt.
ALTER TABLE sessions ADD COLUMN token_lookup_hash TEXT;

-- Postgres treats NULLs as distinct in unique indexes, so legacy rows (with a
-- NULL fingerprint) remain valid and untouched.
CREATE UNIQUE INDEX idx_sessions_token_lookup_hash ON sessions(token_lookup_hash);
