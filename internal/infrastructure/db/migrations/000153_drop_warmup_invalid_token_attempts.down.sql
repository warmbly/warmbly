-- Reverses 000153: the table returns empty, as the baseline created it.
CREATE TABLE public.warmup_invalid_token_attempts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email_account_id uuid NOT NULL,
    attempted_token text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.warmup_invalid_token_attempts
    ADD CONSTRAINT warmup_invalid_token_attempts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.warmup_invalid_token_attempts
    ADD CONSTRAINT warmup_invalid_token_attempts_email_account_id_fkey
    FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

CREATE INDEX idx_warmup_invalid_attempts_account
    ON public.warmup_invalid_token_attempts USING btree (email_account_id, created_at);
