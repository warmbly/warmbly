CREATE TYPE task_type AS ENUM('campaign', 'warmup');
CREATE TYPE task_status AS ENUM('loaded', 'processing', 'completed')

CREATE TABLE tasks (
    id UUID PRIMARY KEY NOT NULL,
    task_type task_type,
    email_account_id UUID REFERENCES email_accounts(id) ON DELETE CASCADE,
    campaign_id UUID REFERENCES campaigns(id) ON DELETE CASCADE,
    lead_id UUID REFERENCES campaign_leads()
)
