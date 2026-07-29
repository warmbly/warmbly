-- Dynamically-registered public clients have no owning org, so the NOT NULL
-- restore below would reject them: remove them first.
DELETE FROM oauth_applications WHERE dynamically_registered = true;

ALTER TABLE oauth_applications
    ALTER COLUMN organization_id SET NOT NULL,
    ALTER COLUMN created_by SET NOT NULL,
    DROP COLUMN is_public,
    DROP COLUMN dynamically_registered;
