-- =============================================================================
-- Rollback: 000001_init_schema
-- Drop in reverse dependency order to satisfy FK constraints.
-- =============================================================================

DROP TABLE IF EXISTS verification_tokens;
DROP TABLE IF EXISTS user_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS client_details;
DROP TABLE IF EXISTS employee_details;
DROP TABLE IF EXISTS users;

DROP EXTENSION IF EXISTS "pgcrypto";
