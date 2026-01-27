-- Remove default free trial plan
DELETE FROM plans WHERE id = '00000000-0000-0000-0000-000000000001';

-- Remove warmup pool type from email accounts
ALTER TABLE email_accounts
DROP COLUMN IF EXISTS warmup_pool_type;

-- Remove trial fields from subscriptions
DROP INDEX IF EXISTS idx_trial_expiry;
ALTER TABLE subscriptions
DROP COLUMN IF EXISTS free_trial_started_at,
DROP COLUMN IF EXISTS free_trial_ends_at;

-- Remove dedicated worker assignments table
DROP TABLE IF EXISTS dedicated_worker_assignments;

-- Remove plan extensions
ALTER TABLE plans
DROP COLUMN IF EXISTS dedicated_workers,
DROP COLUMN IF EXISTS daily_campaign_limit;

-- Remove worker indexes
DROP INDEX IF EXISTS idx_workers_premium_tier;
DROP INDEX IF EXISTS idx_workers_free_tier;
DROP INDEX IF EXISTS idx_workers_shared_load;

-- Remove worker extensions
ALTER TABLE workers
DROP COLUMN IF EXISTS worker_type,
DROP COLUMN IF EXISTS account_count;

-- Remove worker type enum
DROP TYPE IF EXISTS worker_type;
