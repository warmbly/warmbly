-- Reusable worker credentials and runtime profiles.
--
-- aws_credentials: named AWS keypair, one row can be referenced by many
--   worker_profiles, which in turn can be referenced by many workers.
--
-- worker_profiles: a named bundle of everything a worker container needs
--   at runtime besides its identity (kafka, schema registry, redis, AWS
--   reference, image tag, env). One profile can be assigned to many workers.
--
-- All secret material (AWS secret access key, Kafka SASL password, Schema
-- Registry secret, Redis URL) is stored as ciphertext from the cipher
-- service under the platform identity (uuid.Nil). Same envelope encryption
-- as worker SSH private keys.

CREATE TABLE aws_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(120) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    region VARCHAR(40) NOT NULL,
    access_key_id TEXT NOT NULL,
    secret_access_key_encrypted TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE worker_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(120) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    app_env VARCHAR(20) NOT NULL DEFAULT 'prod',
    worker_image TEXT NOT NULL DEFAULT 'ghcr.io/warmbly/worker:latest',

    -- Kafka
    kafka_bootstrap_servers TEXT NOT NULL DEFAULT '',
    kafka_sasl_username TEXT NOT NULL DEFAULT '',
    kafka_sasl_password_encrypted TEXT NOT NULL DEFAULT '',

    -- Schema registry
    schema_registry_url TEXT NOT NULL DEFAULT '',
    schema_registry_key TEXT NOT NULL DEFAULT '',
    schema_registry_secret_encrypted TEXT NOT NULL DEFAULT '',

    -- Redis (URL contains password; encrypt the whole thing)
    redis_url_encrypted TEXT NOT NULL DEFAULT '',

    -- AWS credentials reference. ON DELETE RESTRICT — you cannot delete a
    -- credential row that profiles still depend on.
    aws_credential_id UUID REFERENCES aws_credentials(id) ON DELETE RESTRICT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_worker_profiles_aws ON worker_profiles(aws_credential_id);

-- Workers point to one profile. Detaching is fine (worker falls back to the
-- backend's process-env defaults for dev/sim).
ALTER TABLE workers
    ADD COLUMN profile_id UUID REFERENCES worker_profiles(id) ON DELETE SET NULL,
    -- Set on every successful Install/Apply. Compared to the profile's
    -- updated_at to compute the "stale config" indicator in the UI.
    ADD COLUMN config_applied_at TIMESTAMPTZ;

CREATE INDEX idx_workers_profile ON workers(profile_id);
