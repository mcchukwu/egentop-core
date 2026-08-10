DROP TABLE IF EXISTS authz_decisions;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS membership_status;
DROP TYPE IF EXISTS organization_status;
DROP TYPE IF EXISTS user_status;

DROP FUNCTION IF EXISTS update_updated_at_column();
DROP EXTENSION IF EXISTS "pgcrypto";
