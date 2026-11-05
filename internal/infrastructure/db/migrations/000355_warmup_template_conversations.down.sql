-- Rolling back with workspace templates on file would discard customer data;
-- refuse and leave the operator to roll forward instead.
LOCK TABLE public.warmup_template_settings IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.warmup_conversations WHERE source = 'template')
       OR EXISTS (SELECT 1 FROM public.warmup_template_settings) THEN
        RAISE EXCEPTION 'warmup template rows or settings exist; delete them (archived templates included) before rolling back 000355';
    END IF;
END $$;

ALTER TABLE public.warmup_tokens
    DROP COLUMN IF EXISTS variant_id,
    DROP COLUMN IF EXISTS sequence_id;

DROP TABLE IF EXISTS public.warmup_template_settings;

ALTER TABLE ONLY public.warmup_conversations
    DROP CONSTRAINT warmup_conversations_template_owner_check;

ALTER TABLE ONLY public.warmup_conversations
    DROP CONSTRAINT warmup_conversations_source_check;
ALTER TABLE ONLY public.warmup_conversations
    ADD CONSTRAINT warmup_conversations_source_check
        CHECK ((source = ANY (ARRAY['ai'::text, 'static'::text])));

ALTER TABLE public.warmup_conversations
    DROP COLUMN organization_id;
