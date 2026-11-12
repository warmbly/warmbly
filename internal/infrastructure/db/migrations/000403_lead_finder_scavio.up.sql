-- Scavio joins the lead finder's place sources.
-- Expand only; the previous release never writes 'scavio'.
-- NOT VALID skips the scan: every existing row already passed the narrower CHECK.

-- Drop the provider CHECKs by what they say rather than by an assumed name.
DO $$
DECLARE
    c record;
BEGIN
    FOR c IN
        SELECT con.conname, rel.relname
        FROM pg_constraint con
        JOIN pg_class rel ON rel.oid = con.conrelid
        JOIN pg_namespace ns ON ns.oid = rel.relnamespace
        WHERE con.contype = 'c'
          AND ns.nspname = current_schema()
          AND rel.relname IN ('lead_searches', 'leads')
          AND pg_get_constraintdef(con.oid) LIKE '%provider%'
          AND pg_get_constraintdef(con.oid) LIKE '%outscraper%'
    LOOP
        EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', c.relname, c.conname);
    END LOOP;
END $$;

ALTER TABLE lead_searches
    ADD CONSTRAINT lead_searches_provider_check CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper', 'scavio')) NOT VALID;
ALTER TABLE leads
    ADD CONSTRAINT leads_provider_check CHECK (provider IN ('osm', 'google_places', 'foursquare', 'outscraper', 'scavio')) NOT VALID;
