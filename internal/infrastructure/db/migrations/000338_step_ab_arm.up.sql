-- The step A/B arm each contact was sent, written with the send's reservation.
-- NULL means the send was not part of a split; 'control' is the step's own content.
-- No foreign key: a deleted variant's rows drop out of the stats rather than turning into control.
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS ab_arm text,
    ADD COLUMN IF NOT EXISTS ab_variant_id uuid;

-- NOT VALID skips the scan; every existing row is NULL and new writes are checked.
ALTER TABLE campaign_contact_progress
    DROP CONSTRAINT IF EXISTS campaign_contact_progress_ab_arm_chk;
ALTER TABLE campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_ab_arm_chk CHECK (
        (ab_arm IS NULL AND ab_variant_id IS NULL)
        OR (ab_arm = 'control' AND ab_variant_id IS NULL)
        OR (ab_arm = 'variant' AND ab_variant_id IS NOT NULL)
    ) NOT VALID;

-- When auto-promotion picked this arm as its step's winner; a step is promoted at most once.
ALTER TABLE campaign_ab_variants
    ADD COLUMN IF NOT EXISTS promoted_at timestamptz;
