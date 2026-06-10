-- DEKs are now keyed by organization, not user. Mailboxes, integration
-- connections, and campaign content are organization assets; sealing them
-- under the connecting user's key meant offboarding that user broke every
-- ciphertext they created. Pre-production: no data migration, ciphertexts
-- sealed under the old per-user DEKs are abandoned along with the table.
DROP TABLE IF EXISTS user_encrypted_keys;

-- Deliberately no FK to organizations:
--  * platform-level secrets (worker SSH keys, profile credentials) are stored
--    under the zero UUID, which has no organizations row
--  * losing a DEK is unrecoverable, so rows must never cascade-delete; an
--    orphaned key after org deletion is harmless, a cascaded one is fatal
CREATE TABLE organization_encrypted_keys (
    organization_id uuid PRIMARY KEY,
    encrypted_data_key text NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);
