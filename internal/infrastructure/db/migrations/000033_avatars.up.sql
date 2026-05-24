-- Avatar URLs for users and organizations.
--
-- Object URL lives in DB so reads are free; the actual image lives
-- in S3 (or whichever public bucket the deployment is wired to).

ALTER TABLE users
    ADD COLUMN avatar_url TEXT;

ALTER TABLE organizations
    ADD COLUMN avatar_url TEXT;
