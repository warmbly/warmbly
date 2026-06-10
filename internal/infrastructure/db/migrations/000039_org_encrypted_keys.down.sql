DROP TABLE IF EXISTS organization_encrypted_keys;

CREATE TABLE user_encrypted_keys (
    user_id uuid NOT NULL,
    encrypted_data_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY user_encrypted_keys
    ADD CONSTRAINT user_encrypted_keys_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY user_encrypted_keys
    ADD CONSTRAINT user_encrypted_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
