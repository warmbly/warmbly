DROP INDEX IF EXISTS campaign_ab_variants_unique_name;
DROP INDEX IF EXISTS idx_campaign_ab_variants_sequence;
ALTER TABLE campaign_ab_variants DROP COLUMN IF EXISTS sequence_id;
