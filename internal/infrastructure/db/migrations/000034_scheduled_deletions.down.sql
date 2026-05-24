DROP INDEX IF EXISTS idx_users_pending_deletion;
ALTER TABLE users
    DROP COLUMN IF EXISTS deletion_scheduled_for,
    DROP COLUMN IF EXISTS deletion_scheduled_at;

DROP INDEX IF EXISTS idx_organizations_pending_deletion;
ALTER TABLE organizations
    DROP COLUMN IF EXISTS deletion_scheduled_for,
    DROP COLUMN IF EXISTS deletion_scheduled_at;

DROP TABLE IF EXISTS scheduled_deletions;
