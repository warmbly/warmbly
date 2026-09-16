CREATE INDEX idx_warmup_tokens_message_lookup
    ON warmup_tokens (btrim(sent_message_id, '<>')) WHERE sent_message_id <> '';
CREATE INDEX idx_warmup_received_message_lookup
    ON warmup_received (btrim(message_id, '<>'));

CREATE TABLE unibox_pending_emails (
    id uuid PRIMARY KEY,
    email_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    retry_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_unibox_pending_emails_retry ON unibox_pending_emails (retry_at);
CREATE INDEX idx_unibox_pending_emails_account ON unibox_pending_emails (email_account_id);
