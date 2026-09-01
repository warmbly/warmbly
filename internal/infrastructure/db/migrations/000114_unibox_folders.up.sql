-- Canonical mail folder per unibox message, so the inbox can present the
-- standard folder sidebar (inbox/sent/drafts/archive/spam/trash) instead of
-- one combined list. Written by the sync path from provider placement; the
-- backfills below recover what past syncs recorded elsewhere.
--
-- Cost note: unibox_emails holds every synced message, so on a long-running
-- instance the three backfill passes below are the expensive part of this
-- migration, and the last one runs a correlated address match per row. The
-- backend applies migrations at boot inside one transaction and blocks until
-- they finish, so on a large instance deploy this in a window rather than
-- alongside traffic. ADD COLUMN with a constant DEFAULT is metadata-only on
-- PG 11+ and is not itself a rewrite.
ALTER TABLE unibox_emails
    ADD COLUMN folder text NOT NULL DEFAULT 'inbox';

ALTER TABLE unibox_emails
    ADD CONSTRAINT unibox_emails_folder_check
    CHECK (folder IN ('inbox', 'sent', 'drafts', 'archive', 'spam', 'trash'));

-- IMAP rows: the folder registry kept each source folder's special-use
-- attributes, keyed by (email_id, uid_validity).
UPDATE unibox_emails ue
SET folder = CASE
        WHEN um.attributes && ARRAY['\Trash']          THEN 'trash'
        WHEN um.attributes && ARRAY['\Junk']           THEN 'spam'
        WHEN um.attributes && ARRAY['\Drafts']         THEN 'drafts'
        WHEN um.attributes && ARRAY['\Sent']           THEN 'sent'
        WHEN um.attributes && ARRAY['\Archive','\All'] THEN 'archive'
        ELSE 'inbox'
    END
FROM unibox_mailboxes um
WHERE um.email_id = ue.email_id
  AND um.uid_validity = ue.mailbox
  AND ue.mailbox <> 0;

-- Flag-derived placement wins where the registry had nothing (Gmail/Graph
-- rows carry mailbox = 0 but kept spam and draft pseudo-flags).
UPDATE unibox_emails
SET folder = 'spam'
WHERE folder = 'inbox'
  AND flags && ARRAY['SPAM', '\Junk', '\Spam', 'Junk'];

UPDATE unibox_emails
SET folder = 'drafts'
WHERE folder = 'inbox'
  AND flags && ARRAY['\Draft'];

-- Sent provenance was lost on Gmail/Graph; recover it the way the direction
-- filter always has: our own address in From.
UPDATE unibox_emails ue
SET folder = 'sent'
WHERE ue.folder = 'inbox'
  AND EXISTS (
        SELECT 1
        FROM unnest(ue.from_addr) AS f(addr)
        JOIN email_accounts ea ON ea.id = ue.email_id
        WHERE f.addr ILIKE '%' || ea.email || '%'
    );

CREATE INDEX idx_unibox_emails_folder ON unibox_emails (email_id, folder);
