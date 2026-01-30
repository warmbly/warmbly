-- Daily warmup statistics
CREATE TABLE warmup_statistics (
    email_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    emails_sent INT NOT NULL DEFAULT 0,
    emails_replied INT NOT NULL DEFAULT 0,
    target_volume INT NOT NULL,

    PRIMARY KEY (email_account_id, date)
);

-- Daily email count tracking (for enforcing limits)
CREATE TABLE daily_email_counts (
    email_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    date DATE NOT NULL,
    count INT NOT NULL DEFAULT 0,

    PRIMARY KEY (email_account_id, date)
);

-- Indexes for date range queries
CREATE INDEX idx_warmup_stats_date ON warmup_statistics(date);
CREATE INDEX idx_daily_counts_date ON daily_email_counts(date);
