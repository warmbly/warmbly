-- task_status keeps 'skipped_paused': Postgres cannot drop an enum value.
ALTER TABLE campaign_leads
    DROP CONSTRAINT IF EXISTS campaign_leads_pause_source_check;

ALTER TABLE campaign_leads
    DROP COLUMN IF EXISTS paused_at,
    DROP COLUMN IF EXISTS paused_until,
    DROP COLUMN IF EXISTS pause_reason,
    DROP COLUMN IF EXISTS pause_source;
