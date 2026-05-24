-- Drop indexes
DROP INDEX IF EXISTS idx_enterprise_inquiries_assigned;

-- Remove columns from enterprise_inquiries
ALTER TABLE enterprise_inquiries
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS phone,
    DROP COLUMN IF EXISTS monthly_email_volume,
    DROP COLUMN IF EXISTS message,
    DROP COLUMN IF EXISTS assigned_to,
    DROP COLUMN IF EXISTS updated_at;

-- Drop tables
DROP TABLE IF EXISTS platform_statistics;
DROP TABLE IF EXISTS admin_audit_logs;
DROP TABLE IF EXISTS warmup_appeals;
DROP TABLE IF EXISTS user_bans;
