CREATE TABLE outbound_attempts (
    nonce uuid PRIMARY KEY,
    task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    email_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    provider text NOT NULL,
    attempted_at timestamptz NOT NULL,
    recipient_count integer NOT NULL CHECK (recipient_count > 0)
);
CREATE INDEX outbound_attempts_mailbox_time ON outbound_attempts(email_account_id,attempted_at);
INSERT INTO outbound_attempts(nonce,task_id,email_account_id,provider,attempted_at,recipient_count)
SELECT t.send_executor_nonce,t.id,t.email_account_id,ea.provider::text,t.send_executor_started_at,GREATEST(1,cardinality(t.send_recipients))
FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id
WHERE t.send_executor_started_at IS NOT NULL AND t.send_executor_nonce IS NOT NULL;
ALTER TABLE diagnostic_auth_verifications ADD COLUMN attempts integer NOT NULL DEFAULT 1 CHECK (attempts BETWEEN 1 AND 3);
