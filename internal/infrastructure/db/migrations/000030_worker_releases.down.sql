DROP INDEX IF EXISTS idx_worker_profiles_channel;

ALTER TABLE workers DROP COLUMN IF EXISTS image_version;

ALTER TABLE worker_profiles
    DROP COLUMN IF EXISTS last_release_check_at,
    DROP COLUMN IF EXISTS resolved_image_tag,
    DROP COLUMN IF EXISTS auto_update,
    DROP COLUMN IF EXISTS release_channel;

DROP TYPE IF EXISTS release_channel;
