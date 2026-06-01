-- Record how each session was authenticated (email, google, apple, webauthn)
-- so the account security UI can show "Signed in with …" per device. The
-- provider is already passed into GenerateSession; this gives it a home.
ALTER TABLE sessions
    ADD COLUMN auth_provider TEXT NOT NULL DEFAULT '';
