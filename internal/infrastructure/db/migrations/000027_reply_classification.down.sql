DROP INDEX IF EXISTS idx_campaign_progress_reply_class;

ALTER TABLE campaign_contact_progress
    DROP CONSTRAINT IF EXISTS campaign_contact_progress_reply_source_chk;

ALTER TABLE campaign_contact_progress
    DROP CONSTRAINT IF EXISTS campaign_contact_progress_reply_class_chk;

ALTER TABLE campaign_contact_progress
    DROP COLUMN IF EXISTS reply_source,
    DROP COLUMN IF EXISTS reply_confidence,
    DROP COLUMN IF EXISTS reply_class;
