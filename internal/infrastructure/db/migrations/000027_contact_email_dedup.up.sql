-- Prevent duplicate contacts with the same email for the same user.
-- Uses LOWER(email) for case-insensitive deduplication.
CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_user_email_unique
    ON contacts(user_id, LOWER(email));
