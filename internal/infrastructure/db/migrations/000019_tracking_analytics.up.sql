-- Add indexes for efficient recent activity queries on campaign_contact_progress
CREATE INDEX IF NOT EXISTS idx_campaign_progress_recent_opens
ON campaign_contact_progress(opened_at DESC) WHERE opened_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_campaign_progress_recent_clicks
ON campaign_contact_progress(clicked_at DESC) WHERE clicked_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_campaign_progress_recent_replies
ON campaign_contact_progress(replied_at DESC) WHERE replied_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_campaign_progress_recent_bounces
ON campaign_contact_progress(bounced_at DESC) WHERE bounced_at IS NOT NULL;

-- Add index for tracking lookups by task_id
-- campaign_tasks already has contact_id and sequence_id columns
CREATE INDEX IF NOT EXISTS idx_campaign_tasks_tracking
ON campaign_tasks(task_id) WHERE contact_id IS NOT NULL;

-- Table for tracking event deduplication at consumer level
-- Prevents duplicate processing if Rust service dedupe fails or events are replayed
CREATE TABLE IF NOT EXISTS tracking_events_processed (
    task_id UUID NOT NULL,
    event_type VARCHAR(20) NOT NULL, -- 'opened' or 'clicked'
    ip_hash VARCHAR(32),
    url_hash VARCHAR(16) NOT NULL DEFAULT '', -- For click deduplication per URL
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, event_type, url_hash)
);

-- Auto-cleanup old tracking dedupe records (older than 7 days)
CREATE INDEX IF NOT EXISTS idx_tracking_processed_cleanup
ON tracking_events_processed(processed_at);

-- Add organization_id to campaigns for efficient org-level analytics
-- (This may already exist, IF NOT EXISTS handles that)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'campaigns' AND column_name = 'organization_id'
    ) THEN
        ALTER TABLE campaigns ADD COLUMN organization_id UUID REFERENCES organizations(id);
        CREATE INDEX idx_campaigns_org ON campaigns(organization_id);
    END IF;
END $$;

-- Composite indexes for dashboard analytics queries
CREATE INDEX IF NOT EXISTS idx_campaign_progress_org_sent
ON campaign_contact_progress(campaign_id, sent_at DESC) WHERE sent_at IS NOT NULL;

-- Add index for daily trend queries
CREATE INDEX IF NOT EXISTS idx_campaign_progress_daily
ON campaign_contact_progress(sent_at, campaign_id) WHERE sent_at IS NOT NULL;
