ALTER TABLE organizations
    DROP COLUMN IF EXISTS voice_profile,
    DROP COLUMN IF EXISTS icp_notes,
    DROP COLUMN IF EXISTS product_description;
