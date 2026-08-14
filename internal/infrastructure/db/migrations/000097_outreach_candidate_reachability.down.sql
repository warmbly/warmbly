ALTER TABLE outreach_contact_candidates
    DROP COLUMN IF EXISTS channel_display,
    DROP COLUMN IF EXISTS channel_value,
    DROP COLUMN IF EXISTS route_relation,
    DROP COLUMN IF EXISTS route_type,
    DROP COLUMN IF EXISTS reachability_class;
