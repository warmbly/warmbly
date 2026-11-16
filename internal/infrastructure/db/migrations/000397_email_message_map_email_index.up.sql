-- Alone in its file for CONCURRENTLY. Lets a mailbox delete cascade here by index instead of scanning the table.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_email_message_map_email
    ON public.email_message_map (email_id);
