-- Warmup tampering protection.
--
-- Verified warmup mail is NOT stored in the unibox (handleWarmupEmail returns
-- "handled" before CreateEntry), so there is otherwise no record that a given
-- message in a recipient's mailbox was a warmup email. Without that, a later
-- flag-change (mark-as-spam) or deletion event — which only carries the
-- internal message id — can't be attributed to warmup.
--
--  1. warmup_received   — records every verified warmup email delivered to a
--                         participant, keyed by (recipient mailbox, internal
--                         message id) so a later delete/flag event can be
--                         matched back to warmup and to the sender.
--  2. warmup_tampering_events — one row per "harm" a participant does to a
--                         warmup email (deleted it, or marked it as spam).
--                         Crossing the threshold bans the mailbox from warmup
--                         (BlockFromPool); the user can then appeal
--                         (warmup_appeals, already present).

CREATE TABLE public.warmup_received (
    email_account_id uuid NOT NULL,
    internal_id uuid NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    sender_account_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT warmup_received_pkey PRIMARY KEY (email_account_id, internal_id)
);

CREATE INDEX idx_warmup_received_created ON public.warmup_received USING btree (created_at);

CREATE TABLE public.warmup_tampering_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email_account_id uuid NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    kind text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT warmup_tampering_events_pkey PRIMARY KEY (id),
    CONSTRAINT warmup_tampering_events_uniq UNIQUE (email_account_id, message_id, kind),
    CONSTRAINT warmup_tampering_events_kind_check CHECK ((kind = ANY (ARRAY['deletion'::text, 'spam_flag'::text, 'other'::text])))
);

CREATE INDEX idx_warmup_tampering_account_created ON public.warmup_tampering_events USING btree (email_account_id, created_at DESC);
