DROP INDEX IF EXISTS idx_workers_profile;

ALTER TABLE workers
    DROP COLUMN IF EXISTS config_applied_at,
    DROP COLUMN IF EXISTS profile_id;

DROP INDEX IF EXISTS idx_worker_profiles_aws;
DROP TABLE IF EXISTS worker_profiles;
DROP TABLE IF EXISTS aws_credentials;
