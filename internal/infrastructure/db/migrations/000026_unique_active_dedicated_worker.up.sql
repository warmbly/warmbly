-- Enforce one active dedicated worker per user at the database level.
-- The existing UNIQUE (user_id, released_at) doesn't prevent duplicates
-- when released_at IS NULL because PostgreSQL treats NULLs as distinct.
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_active_dedicated_user
    ON dedicated_worker_assignments(user_id) WHERE released_at IS NULL;
