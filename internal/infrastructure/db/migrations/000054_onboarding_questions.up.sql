-- Multi-step onboarding questionnaire: the user's role/persona and team size,
-- captured after the profile + workspace-naming steps. Both nullable, so any
-- pre-existing user simply keeps NULL until they complete onboarding again.
-- (Named job_role to avoid colliding with the org-membership "role" concept.)
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS job_role  VARCHAR(50),
    ADD COLUMN IF NOT EXISTS team_size VARCHAR(20);
