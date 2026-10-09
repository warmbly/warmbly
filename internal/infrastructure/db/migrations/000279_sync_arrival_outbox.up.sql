CREATE TABLE sync_arrival_outbox (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    message_id text NOT NULL,
    id uuid NOT NULL,
    payload text NOT NULL,
    stage smallint NOT NULL DEFAULT 0 CHECK (stage BETWEEN 0 AND 3),
    lease uuid,
    locked_until timestamptz,
    retry_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, email_id, message_id),
    FOREIGN KEY (user_id, email_id, message_id)
        REFERENCES email_message_map(user_id, email_id, message_id) ON DELETE CASCADE
);
CREATE INDEX sync_arrival_outbox_due ON sync_arrival_outbox(retry_at, created_at);
CREATE INDEX sync_arrival_outbox_pending ON sync_arrival_outbox(user_id, email_id, id) WHERE stage = 0;
