ALTER TABLE campaign_leads
    DROP CONSTRAINT IF EXISTS campaign_leads_source_check;

ALTER TABLE campaign_leads
    DROP COLUMN IF EXISTS source;
