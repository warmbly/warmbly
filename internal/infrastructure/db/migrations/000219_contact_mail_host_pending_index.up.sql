-- Alone in its file for CONCURRENTLY. The provider sweep reads contacts whose
-- host is not known yet, oldest check first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contacts_mail_host_pending
    ON contacts (esp_resolved_at NULLS FIRST)
    WHERE mail_host = '';
