CREATE TABLE warmup_conversation_mailboxes (
    conversation_id uuid NOT NULL REFERENCES warmup_conversations(id) ON DELETE CASCADE,
    email_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, email_account_id),
    UNIQUE (task_id, email_account_id)
);

CREATE INDEX warmup_conversation_mailboxes_account_idx
    ON warmup_conversation_mailboxes (email_account_id);

-- Preserve historical exposure as tokens are retired, without a deployment-time backfill.
CREATE FUNCTION preserve_warmup_conversation_exposure() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO warmup_conversation_mailboxes (conversation_id, email_account_id)
    SELECT c.id, ea.id
    FROM warmup_conversations c
    JOIN email_accounts ea ON ea.id IN (OLD.sender_account_id, OLD.recipient_account_id)
    WHERE c.id = OLD.conversation_id
    ORDER BY ea.id
    ON CONFLICT DO NOTHING;
    RETURN NULL;
END;
$$;

CREATE TRIGGER preserve_warmup_conversation_exposure
AFTER DELETE OR UPDATE OF conversation_id, sender_account_id, recipient_account_id ON warmup_tokens
FOR EACH ROW EXECUTE FUNCTION preserve_warmup_conversation_exposure();
