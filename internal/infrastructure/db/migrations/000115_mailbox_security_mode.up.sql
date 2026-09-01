-- Explicit connection security per mailbox, so a mailbox can live on any port
-- instead of only the four the code could infer a mode from. Backfilled from
-- the port each row already uses, which preserves current behavior for every
-- standard setup and repairs IMAP mailboxes stored on 143 (those could never
-- connect: the client always dialed implicit TLS).
ALTER TABLE email_accounts_smtp_imap
    ADD COLUMN smtp_security text NOT NULL DEFAULT 'starttls',
    ADD COLUMN imap_security text NOT NULL DEFAULT 'tls';

UPDATE email_accounts_smtp_imap
SET smtp_security = CASE WHEN smtp_port = 465 THEN 'tls' ELSE 'starttls' END,
    imap_security = CASE WHEN imap_port = 143 THEN 'starttls' ELSE 'tls' END;

ALTER TABLE email_accounts_smtp_imap
    ADD CONSTRAINT email_accounts_smtp_imap_smtp_security_check
    CHECK (smtp_security IN ('tls', 'starttls'));

ALTER TABLE email_accounts_smtp_imap
    ADD CONSTRAINT email_accounts_smtp_imap_imap_security_check
    CHECK (imap_security IN ('tls', 'starttls'));
