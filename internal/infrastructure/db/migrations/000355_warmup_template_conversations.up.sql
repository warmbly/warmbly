-- Fail the deploy rather than queue warmup traffic behind a lock we cannot get.
SET LOCAL lock_timeout = '10s';

-- Organization-scoped warmup templates live in the shared content bank:
-- a row an organization submits itself (subject plus opening body) that the
-- pick pipeline interleaves into that organization's own warmup sends.
ALTER TABLE public.warmup_conversations
    ADD COLUMN organization_id uuid REFERENCES public.organizations(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.warmup_conversations
    DROP CONSTRAINT warmup_conversations_source_check;
ALTER TABLE ONLY public.warmup_conversations
    ADD CONSTRAINT warmup_conversations_source_check
        CHECK ((source = ANY (ARRAY['ai'::text, 'static'::text, 'template'::text])));

-- A template always has an owner; shared library rows never do.
ALTER TABLE ONLY public.warmup_conversations
    ADD CONSTRAINT warmup_conversations_template_owner_check
        CHECK ((source = 'template') = (organization_id IS NOT NULL));

-- How much of a workspace's warmup carries its own copy (templates and the
-- opening steps of its running campaigns). One row per organization; absence
-- means the defaults.
CREATE TABLE public.warmup_template_settings (
    organization_id uuid PRIMARY KEY REFERENCES public.organizations(id) ON DELETE CASCADE,
    template_share integer NOT NULL DEFAULT 25 CHECK (template_share BETWEEN 0 AND 100),
    templates_only boolean NOT NULL DEFAULT false,
    campaign_copy boolean NOT NULL DEFAULT true,
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

-- Which campaign step (and A/B arm) a content_source='campaign' send carried.
-- Nullable and unconstrained: a deleted step leaves its history unattributed.
ALTER TABLE public.warmup_tokens
    ADD COLUMN sequence_id uuid,
    ADD COLUMN variant_id uuid;
