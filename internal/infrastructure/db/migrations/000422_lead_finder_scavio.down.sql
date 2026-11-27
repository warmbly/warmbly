-- Narrow the provider CHECKs back without touching rows. NOT VALID leaves any
-- Scavio searches and leads in place, so this always runs (as in 000363).
ALTER TABLE lead_searches DROP CONSTRAINT IF EXISTS lead_searches_provider_check;
ALTER TABLE lead_searches
    ADD CONSTRAINT lead_searches_provider_check CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper')) NOT VALID;
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_provider_check;
ALTER TABLE leads
    ADD CONSTRAINT leads_provider_check CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper')) NOT VALID;
