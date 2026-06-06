-- Per-step A/B variants. A variant may belong to a specific sequence step
-- (sequence_id). NULL = campaign-level (legacy: applies regardless of step).
-- Step-scoped variants are selected deterministically per (contact, step), so
-- they need no assignment-table changes; campaign-level keeps its assignment.
ALTER TABLE campaign_ab_variants ADD COLUMN IF NOT EXISTS sequence_id uuid REFERENCES sequences(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS idx_campaign_ab_variants_sequence ON campaign_ab_variants (sequence_id);

-- Variant names are unique per (campaign, step). COALESCE the NULL step to a
-- sentinel so campaign-level names also stay unique (NULLs would otherwise be
-- distinct in a plain unique index).
ALTER TABLE campaign_ab_variants DROP CONSTRAINT IF EXISTS campaign_variant_unique_name;
CREATE UNIQUE INDEX IF NOT EXISTS campaign_ab_variants_unique_name
    ON campaign_ab_variants (campaign_id, COALESCE(sequence_id, '00000000-0000-0000-0000-000000000000'::uuid), name);
