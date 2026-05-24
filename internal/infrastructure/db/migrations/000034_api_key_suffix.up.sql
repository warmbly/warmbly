-- API key display suffix: last 4 chars of the raw key.
-- Together with key_prefix this gives users enough to identify a key
-- in the UI (e.g. "wmbly_ab...wxyz") without exposing the secret.

ALTER TABLE api_keys
    ADD COLUMN key_suffix VARCHAR(4) NOT NULL DEFAULT '';
