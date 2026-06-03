-- Warmup content system.
--
-- The live warmup send flow previously drew message bodies from a small,
-- fixed in-code library (~30 conversations). Reusing the same handful of
-- bodies across every mailbox in the pool is the documented content-
-- fingerprinting risk: collaborative bulk-checksum systems (DCC/Razor/Pyzor)
-- and provider ML learn to recognise the "Warmbly warmup dialect". This
-- migration adds the control-plane plumbing to fix that:
--
--  1. warmup_conversations    — a DB-backed bank of conversation threads that
--                               an offline generator (OpenAI, run as a job)
--                               continuously refills per pool + segment. The
--                               static in-code library remains the fallback,
--                               so an outage or empty bank never stops warmup.
--  2. warmup_generation_jobs  — observability for every generation run
--                               (manual or scheduled): how many were asked
--                               for, generated, lint-rejected, failed.
--  3. admin_settings          — a generic key/value JSON settings store; the
--                               warmup generation + engagement config lives
--                               under the 'warmup_generation' key so admins
--                               have full control over volume, cadence, model,
--                               per-pool segments and engagement rates.
--  4. content-cohort columns  — content_source / conversation_id on
--                               warmup_tokens and content_source on
--                               warmup_spam_reports, so the A/B harness can
--                               compare spam-placement rate by content cohort
--                               (static vs AI) without fragile time-window joins.

CREATE TABLE public.warmup_conversations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    pool_type text NOT NULL,
    segment text DEFAULT ''::text NOT NULL,
    source text NOT NULL,
    theme text DEFAULT ''::text NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    messages jsonb DEFAULT '[]'::jsonb NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    lint_passed boolean DEFAULT true NOT NULL,
    usage_count bigint DEFAULT 0 NOT NULL,
    generated_by_job_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT warmup_conversations_pkey PRIMARY KEY (id),
    CONSTRAINT warmup_conversations_pool_type_check CHECK ((pool_type = ANY (ARRAY['free'::text, 'premium'::text]))),
    CONSTRAINT warmup_conversations_source_check CHECK ((source = ANY (ARRAY['ai'::text, 'static'::text]))),
    CONSTRAINT warmup_conversations_status_check CHECK ((status = ANY (ARRAY['active'::text, 'archived'::text])))
);

CREATE INDEX idx_warmup_conversations_pick ON public.warmup_conversations USING btree (pool_type, segment, status);
CREATE INDEX idx_warmup_conversations_source ON public.warmup_conversations USING btree (source);
CREATE INDEX idx_warmup_conversations_created ON public.warmup_conversations USING btree (created_at DESC);

CREATE TABLE public.warmup_generation_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    requested_by uuid,
    trigger text DEFAULT 'manual'::text NOT NULL,
    pool_type text DEFAULT ''::text NOT NULL,
    segment text DEFAULT ''::text NOT NULL,
    theme text DEFAULT ''::text NOT NULL,
    model text DEFAULT ''::text NOT NULL,
    requested_count integer DEFAULT 0 NOT NULL,
    generated_count integer DEFAULT 0 NOT NULL,
    lint_rejected_count integer DEFAULT 0 NOT NULL,
    failed_count integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    error text DEFAULT ''::text NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT warmup_generation_jobs_pkey PRIMARY KEY (id)
);

CREATE INDEX idx_warmup_generation_jobs_created ON public.warmup_generation_jobs USING btree (created_at DESC);
CREATE INDEX idx_warmup_generation_jobs_status ON public.warmup_generation_jobs USING btree (status);

CREATE TABLE public.admin_settings (
    key text NOT NULL,
    value jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT admin_settings_pkey PRIMARY KEY (key)
);

ALTER TABLE ONLY public.warmup_tokens
    ADD COLUMN content_source text DEFAULT ''::text NOT NULL;
ALTER TABLE ONLY public.warmup_tokens
    ADD COLUMN conversation_id uuid;

ALTER TABLE ONLY public.warmup_spam_reports
    ADD COLUMN content_source text DEFAULT ''::text NOT NULL;

CREATE INDEX idx_warmup_tokens_cohort ON public.warmup_tokens USING btree (content_source, created_at DESC);
CREATE INDEX idx_warmup_spam_reports_cohort ON public.warmup_spam_reports USING btree (content_source, report_type, created_at DESC);
