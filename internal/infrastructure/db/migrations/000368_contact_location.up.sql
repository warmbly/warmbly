-- Where a contact is, worked out by a backend sweep from what the record
-- already carries (timezone, country, state, city or location fields, phone,
-- email domain). Derived, never typed in: the fingerprint is the inputs it was
-- read from, so a contact whose inputs change is read again.
CREATE FUNCTION public.contact_location_fingerprint(fields jsonb, phone text, email text)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, pg_temp
AS $$
    SELECT md5(fields::text || chr(31) || phone || chr(31) || email)
$$;

-- NOT VALID: every existing row holds the defaults.
ALTER TABLE contacts
    ADD COLUMN location_timezone text NOT NULL DEFAULT '',
    ADD COLUMN location_country text NOT NULL DEFAULT '',
    ADD COLUMN location_source text NOT NULL DEFAULT '',
    ADD COLUMN location_fingerprint text,
    ADD CONSTRAINT contacts_location_timezone_check CHECK (length(location_timezone) <= 64) NOT VALID,
    ADD CONSTRAINT contacts_location_country_check CHECK (location_country ~ '^([A-Z]{2})?$') NOT VALID,
    ADD CONSTRAINT contacts_location_source_check
        CHECK (location_source IN ('', 'timezone', 'city', 'region', 'country', 'phone', 'email_domain')) NOT VALID;

-- campaign: the schedule is read in the campaign's timezone. recipient: in
-- each lead's own, falling back to the campaign's when it is not known.
ALTER TABLE campaigns
    ADD COLUMN timezone_mode text NOT NULL DEFAULT 'campaign',
    ADD CONSTRAINT campaigns_timezone_mode_check CHECK (timezone_mode IN ('campaign', 'recipient'));
