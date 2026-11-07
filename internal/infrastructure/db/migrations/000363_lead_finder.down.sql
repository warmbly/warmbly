DROP TABLE IF EXISTS lead_people;
DROP TABLE IF EXISTS lead_enrichment_runs;
DROP TABLE IF EXISTS lead_search_hits;
DROP TABLE IF EXISTS leads;
DROP TABLE IF EXISTS lead_searches;

ALTER TABLE public.contacts DROP CONSTRAINT contacts_source_check;
ALTER TABLE public.contacts
    ADD CONSTRAINT contacts_source_check
    CHECK (source IN ('unknown', 'manual', 'campaign', 'import', 'sheet_sync', 'api', 'ai_assistant', 'form', 'automation', 'crm_sync', 'apollo', 'clay')) NOT VALID;
