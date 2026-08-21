-- Give SMTP/IMAP mailboxes a Sent folder copy of what Warmbly sends.
--
-- Gmail and Outlook file a sent copy themselves: their APIs put the message in
-- the provider's Sent folder as part of the send. Plain SMTP does not. Warmbly
-- never issued an IMAP APPEND after an SMTP send, so for an SMTP/IMAP mailbox
-- the message left the building and nothing in the account ever showed it:
-- not the provider's own mail client, and not the unibox, whose thread reader
-- can only show messages the sync found in a folder.
--
-- The column is a per-mailbox choice rather than a global one because both
-- server behaviours are common. A submission server that files the copy itself
-- (Gmail's SMTP, Fastmail, Zoho) would end up with two copies if Warmbly also
-- appended one, which is exactly why every desktop mail client ships this same
-- switch. Default on: the majority of SMTP hosts used for cold outreach
-- (Postfix, Mailcow, cPanel) file nothing.
ALTER TABLE email_accounts
    ADD COLUMN save_to_sent boolean NOT NULL DEFAULT true;

COMMENT ON COLUMN email_accounts.save_to_sent IS
    'SMTP/IMAP only: APPEND a copy of each sent message to the mailbox Sent folder. Turn off when the submission server files its own copy.';
