-- Whether each campaign send carried an open pixel and tracked links, so a rate counts only sends that could register one.
-- Nullable with no default: existing rows are left as unknown without a table rewrite, and readers fall back for them.
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS open_tracked boolean,
    ADD COLUMN IF NOT EXISTS click_tracked boolean;
