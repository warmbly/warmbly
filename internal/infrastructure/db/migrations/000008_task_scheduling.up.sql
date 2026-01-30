-- Add scheduling columns to tasks table
ALTER TABLE tasks ADD COLUMN scheduled_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN completed_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN cloud_task_name TEXT;

-- Add indexes for scheduling queries (critical for performance)
CREATE INDEX idx_tasks_account_scheduled
ON tasks(email_account_id, scheduled_at)
WHERE status = 'pending';

CREATE INDEX idx_tasks_account_completed
ON tasks(email_account_id, completed_at)
WHERE status = 'completed';

CREATE INDEX idx_tasks_scheduled_date
ON tasks(DATE(scheduled_at))
WHERE status = 'pending';

-- Add encryption flag to email_tasks
ALTER TABLE email_tasks ADD COLUMN encrypted BOOLEAN NOT NULL DEFAULT TRUE;

-- Add foreign key for worker_id on email_accounts (workers table created in 000005)
ALTER TABLE email_accounts ADD CONSTRAINT fk_email_accounts_worker
    FOREIGN KEY (worker_id) REFERENCES workers(id) ON DELETE SET NULL;

-- Add index for worker lookups
CREATE INDEX idx_email_accounts_worker ON email_accounts(worker_id) WHERE worker_id IS NOT NULL;

-- Index for counting campaign-only emails (excluding warmup)
CREATE INDEX idx_tasks_campaign_completed_today
ON tasks(email_account_id, completed_at)
WHERE status = 'completed' AND task_type = 'campaign';
