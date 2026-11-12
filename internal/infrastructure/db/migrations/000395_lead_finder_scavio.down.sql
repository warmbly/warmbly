-- Refuse rather than delete searches and leads a workspace may already hold.
-- The locks hold to the end of the migration's transaction, so no Scavio row
-- can land between the check and the narrower CHECKs (as in 000331).
DO $$
BEGIN
    LOCK TABLE lead_searches IN ACCESS EXCLUSIVE MODE;
    LOCK TABLE leads IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM lead_searches WHERE provider = 'scavio')
        OR EXISTS (SELECT 1 FROM leads WHERE provider = 'scavio') THEN
        RAISE EXCEPTION 'lead finder holds Scavio searches or leads, written after 000395; roll forward instead of downgrading past it';
    END IF;
END $$;

ALTER TABLE lead_searches DROP CONSTRAINT IF EXISTS lead_searches_provider_check;
ALTER TABLE lead_searches
    ADD CONSTRAINT lead_searches_provider_check CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper')) NOT VALID;
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_provider_check;
ALTER TABLE leads
    ADD CONSTRAINT leads_provider_check CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper')) NOT VALID;
