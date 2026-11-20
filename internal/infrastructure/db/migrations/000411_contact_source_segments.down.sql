DROP TABLE IF EXISTS public.form_segments;
ALTER TABLE public.salesforce_import_sources DROP COLUMN IF EXISTS segment_ids;
ALTER TABLE public.apollo_import_sources DROP COLUMN IF EXISTS segment_ids;
