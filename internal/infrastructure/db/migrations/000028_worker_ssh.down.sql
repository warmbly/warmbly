DROP INDEX IF EXISTS idx_workers_enrollment_token;
DROP INDEX IF EXISTS idx_workers_install_state;

ALTER TABLE workers
    DROP COLUMN IF EXISTS enrollment_token_expires_at,
    DROP COLUMN IF EXISTS enrollment_token_hash,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS last_seen_at,
    DROP COLUMN IF EXISTS install_state,
    DROP COLUMN IF EXISTS ssh_host_fingerprint,
    DROP COLUMN IF EXISTS ssh_private_key_encrypted,
    DROP COLUMN IF EXISTS ssh_public_key,
    DROP COLUMN IF EXISTS ssh_user,
    DROP COLUMN IF EXISTS ssh_port,
    DROP COLUMN IF EXISTS ssh_host;

DROP TYPE IF EXISTS worker_install_state;
