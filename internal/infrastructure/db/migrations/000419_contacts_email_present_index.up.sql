-- Alone in its file for CONCURRENTLY: contacts is a live table. The unique email index for contacts that
-- have an email; a later release drops idx_contacts_user_email_unique so contacts without one can exist.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_contacts_user_email_present
    ON contacts (user_id, lower(email))
    WHERE email <> '';
