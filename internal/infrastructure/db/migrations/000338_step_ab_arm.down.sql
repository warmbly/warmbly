ALTER TABLE campaign_ab_variants
    DROP COLUMN IF EXISTS promoted_at;
ALTER TABLE campaign_contact_progress
    DROP CONSTRAINT IF EXISTS campaign_contact_progress_ab_arm_chk;
ALTER TABLE campaign_contact_progress
    DROP COLUMN IF EXISTS ab_variant_id,
    DROP COLUMN IF EXISTS ab_arm;
