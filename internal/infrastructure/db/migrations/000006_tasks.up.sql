CREATE TYPE task_type AS ENUM('campaign', 'warmup', 'email');
CREATE TYPE task_status AS ENUM(
    'pending',
    'active',
    'completed',
    'failed',
    'cancelled',
    'skipped_trial_expired',
    'skipped_daily_limit',
    'skipped_suppressed',
    'skipped_no_warmup_access',
    'dead_lettered'
);

CREATE TABLE tasks (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    task_type task_type NOT NULL,
    email_account_id UUID NOT NULL,
    status task_status NOT NULL DEFAULT 'pending',

    message_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (id),
    FOREIGN KEY (email_account_id) REFERENCES email_accounts(id)
);

CREATE TABLE campaign_tasks (
    task_id UUID NOT NULL,
    campaign_id UUID,
    contact_id UUID,
    sequence_id UUID,

    PRIMARY KEY (task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE SET NULL,
    FOREIGN KEY (contact_id) REFERENCES contacts(id) ON DELETE SET NULL,
    FOREIGN KEY (sequence_id) REFERENCES sequences(id) ON DELETE SET NULL
);

CREATE TABLE warmup_tasks (
    task_id UUID NOT NULL,
    target_account_id UUID,

    PRIMARY KEY (task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    FOREIGN KEY (target_account_id) REFERENCES email_accounts(id) ON DELETE SET NULL
);

CREATE TABLE email_tasks (
    task_id UUID NOT NULL,

    to_addrs TEXT[],
    cc TEXT[],
    bcc TEXT[],
    in_reply_to TEXT[],
    subject TEXT NOT NULL,
    body TEXT NOT NULL,
    body_html TEXT NOT NULL DEFAULT '',
    body_plain TEXT NOT NULL DEFAULT '',
    thread_id TEXT,
    send_mode VARCHAR(20) NOT NULL DEFAULT 'instant',

    PRIMARY KEY (task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_email_tasks_thread ON email_tasks(thread_id) WHERE thread_id IS NOT NULL;

CREATE TABLE task_failures (
    task_id UUID NOT NULL,

    title TEXT NOT NULL,
    message TEXT NOT NULL,

    PRIMARY KEY (task_id),
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
