-- Alone in its file for CONCURRENTLY. Lets a mailbox delete cascade here by index instead of scanning the table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_inbox_tag_results_email_account
    ON public.inbox_tag_results (email_account_id);
