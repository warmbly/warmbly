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

-- Marks a receipt as counted in the rollup, so each is counted exactly once.
-- Receipts already on file start uncounted; the consumer's placement sweep
-- rolls them up in batches, which is also what history is seeded from.
ALTER TABLE warmup_received ADD COLUMN placed boolean NOT NULL DEFAULT false;
