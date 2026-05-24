-- SSH-driven worker management.
--
-- Lets admins add a VPS by SSH, encrypt the per-worker private key via the
-- existing cipher service, track install state, and surface liveness from
-- Kafka heartbeats.

CREATE TYPE worker_install_state AS ENUM (
    'pending',
    'provisioning',
    'installed',
    'error',
    'uninstalling',
    'uninstalled'
);

ALTER TABLE workers
    ADD COLUMN ssh_host TEXT,
    ADD COLUMN ssh_port INT NOT NULL DEFAULT 22,
    ADD COLUMN ssh_user VARCHAR(64) NOT NULL DEFAULT 'root',
    ADD COLUMN ssh_public_key TEXT,
    ADD COLUMN ssh_private_key_encrypted TEXT,
    ADD COLUMN ssh_host_fingerprint TEXT,
    ADD COLUMN install_state worker_install_state NOT NULL DEFAULT 'pending',
    ADD COLUMN last_seen_at TIMESTAMPTZ,
    ADD COLUMN last_error TEXT,
    ADD COLUMN enrollment_token_hash TEXT,
    ADD COLUMN enrollment_token_expires_at TIMESTAMPTZ;

CREATE INDEX idx_workers_install_state ON workers(install_state);
CREATE INDEX idx_workers_enrollment_token ON workers(enrollment_token_hash)
    WHERE enrollment_token_hash IS NOT NULL;
