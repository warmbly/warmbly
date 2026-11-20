-- Segment targets for saved Apollo and Salesforce imports and for forms, so
-- every contact source can pin what it brings in into segments, as file
-- imports, Google Sheets syncs and Clay already do.

-- uuid[] like category_ids beside it: handed to the importer, which skips a
-- segment deleted since, and never filtered in SQL.
ALTER TABLE public.apollo_import_sources
    ADD COLUMN segment_ids uuid[] NOT NULL DEFAULT '{}';
ALTER TABLE public.salesforce_import_sources
    ADD COLUMN segment_ids uuid[] NOT NULL DEFAULT '{}';

-- Segments a form submission pins its contact into, mirroring form_categories.
CREATE TABLE public.form_segments (
    form_id uuid NOT NULL REFERENCES public.forms(id) ON DELETE CASCADE,
    segment_id uuid NOT NULL REFERENCES public.segments(id) ON DELETE CASCADE,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (form_id, segment_id)
);

CREATE INDEX idx_form_segments_segment ON public.form_segments (segment_id);
