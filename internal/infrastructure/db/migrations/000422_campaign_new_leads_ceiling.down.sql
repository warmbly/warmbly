-- The old ceiling refuses values above 1000, so clamp them before restoring it.
ALTER TABLE campaigns DROP CONSTRAINT IF EXISTS campaigns_max_new_leads_per_day_range;
UPDATE campaigns SET max_new_leads_per_day = 1000 WHERE max_new_leads_per_day > 1000;
ALTER TABLE campaigns ADD CONSTRAINT campaigns_max_new_leads_per_day_check
    CHECK (max_new_leads_per_day >= 0 AND max_new_leads_per_day <= 1000);
