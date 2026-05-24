-- Auto-update on GitHub release.
--
-- Each worker_profile can subscribe to a release channel. The backend resolves
-- the channel to a concrete image tag (pulled from a GitHub repo's releases)
-- and, if auto_update is on, rolls every assigned worker to the new image.
--
-- workers.image_version records the tag a worker is currently running so the
-- dashboard can show "v1.2.3 → v1.2.4" diffs.

CREATE TYPE release_channel AS ENUM ('pinned', 'stable', 'dev');

ALTER TABLE worker_profiles
    ADD COLUMN release_channel release_channel NOT NULL DEFAULT 'pinned',
    ADD COLUMN auto_update BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN resolved_image_tag TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_release_check_at TIMESTAMPTZ;

ALTER TABLE workers
    ADD COLUMN image_version TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_worker_profiles_channel ON worker_profiles(release_channel) WHERE release_channel <> 'pinned';
