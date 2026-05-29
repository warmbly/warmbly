-- Per-user envelope-encrypted DEK storage for the Postgres backend of
-- internal/infrastructure/encryptedkeys. Replaces the AWS-only DynamoDB
-- "UserEncryptedKeys" table for self-hosted deployments.
--
-- The encrypted_data_key column stores the base64-encoded KMS-encrypted DEK.
-- Plaintext DEKs never touch this table; they live in process memory and a
-- short-TTL Redis cache.
--
-- The cipher service treats the absence of a row as "generate a new DEK",
-- so PostgresStore.Get returns "" rather than an error on no-rows.

CREATE TABLE IF NOT EXISTS user_encrypted_keys (
    user_id            UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    encrypted_data_key TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
