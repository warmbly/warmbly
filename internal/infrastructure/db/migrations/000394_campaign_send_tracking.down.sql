ALTER TABLE campaign_contact_progress
    DROP COLUMN IF EXISTS open_tracked,
    DROP COLUMN IF EXISTS click_tracked;
