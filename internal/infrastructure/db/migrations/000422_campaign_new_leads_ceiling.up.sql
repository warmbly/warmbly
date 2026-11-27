-- Raise the max_new_leads_per_day ceiling from 1000 to 10000 (config.CampaignMaxNewLeadsMax).
-- The 000013 CHECK was unnamed, so drop it by definition rather than by its generated name.
DO $$
DECLARE c record;
BEGIN
    FOR c IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'campaigns'::regclass AND contype = 'c'
          AND pg_get_constraintdef(oid) LIKE '%max_new_leads_per_day%'
    LOOP
        EXECUTE format('ALTER TABLE campaigns DROP CONSTRAINT %I', c.conname);
    END LOOP;
END $$;

ALTER TABLE campaigns ADD CONSTRAINT campaigns_max_new_leads_per_day_range
    CHECK (max_new_leads_per_day >= 0 AND max_new_leads_per_day <= 10000) NOT VALID;
ALTER TABLE campaigns VALIDATE CONSTRAINT campaigns_max_new_leads_per_day_range;
