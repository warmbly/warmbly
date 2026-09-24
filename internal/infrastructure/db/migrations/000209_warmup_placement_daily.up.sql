-- Where each sender's warmup mail landed, one row per sender, UTC day and
-- recipient host. Kept for the life of the mailbox, like warmup_statistics,
-- so placement history outlives the per-message receipts the retention sweep prunes.
CREATE TABLE warmup_placement_daily (
    sender_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    date date NOT NULL,
    recipient_group text NOT NULL CHECK (recipient_group IN ('google', 'microsoft', 'yahoo', 'other')),
    recipient_host text NOT NULL DEFAULT '' CHECK (recipient_host ~ '^[a-z0-9_]{0,32}$'),
    inbox integer NOT NULL DEFAULT 0 CHECK (inbox >= 0),
    tabs integer NOT NULL DEFAULT 0 CHECK (tabs >= 0),
    spam integer NOT NULL DEFAULT 0 CHECK (spam >= 0),
    rescued integer NOT NULL DEFAULT 0 CHECK (rescued >= 0),
    PRIMARY KEY (sender_account_id, date, recipient_group, recipient_host)
);

CREATE INDEX idx_warmup_placement_daily_date ON warmup_placement_daily (date);

-- Marks a receipt as counted in the rollup, so a redelivered arrival is counted
-- once. Rows already on file are seeded below, hence true; new rows start false.
ALTER TABLE warmup_received ADD COLUMN placed boolean NOT NULL DEFAULT true;
ALTER TABLE warmup_received ALTER COLUMN placed SET DEFAULT false;

-- Seed from the receipts still on file. Category tabs and rescues were never
-- recorded, so history before this migration reads them as inbox and zero.
INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, spam)
SELECT
    wr.sender_account_id,
    (wr.created_at AT TIME ZONE 'UTC')::date,
    CASE
        WHEN rec.mail_host IN ('google_workspace', 'gmail') THEN 'google'
        WHEN rec.mail_host IN ('microsoft365', 'outlook') THEN 'microsoft'
        WHEN rec.mail_host IN ('yahoo', 'aol') THEN 'yahoo'
        WHEN rec.mail_host = '' AND rec.provider = 'gmail' THEN 'google'
        WHEN rec.mail_host = '' AND rec.provider = 'outlook' THEN 'microsoft'
        ELSE 'other'
    END,
    rec.mail_host,
    COUNT(*) FILTER (WHERE sr.id IS NULL),
    COUNT(*) FILTER (WHERE sr.id IS NOT NULL)
FROM warmup_received wr
JOIN email_accounts rec ON rec.id = wr.email_account_id
JOIN email_accounts snd ON snd.id = wr.sender_account_id
LEFT JOIN warmup_spam_reports sr
    ON sr.reporter_account_id = wr.email_account_id
   AND sr.message_id = wr.message_id
   AND sr.report_type = 'spam_placement'
   AND wr.message_id <> ''
GROUP BY 1, 2, 3, 4;
