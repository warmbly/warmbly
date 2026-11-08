ALTER TABLE campaigns DROP COLUMN IF EXISTS timezone_mode;

ALTER TABLE contacts
    DROP COLUMN IF EXISTS location_fingerprint,
    DROP COLUMN IF EXISTS location_source,
    DROP COLUMN IF EXISTS location_country,
    DROP COLUMN IF EXISTS location_timezone;

DROP FUNCTION IF EXISTS public.contact_location_fingerprint(jsonb, text, text);
