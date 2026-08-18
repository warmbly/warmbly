-- email_sync_state: what the platform knows about one mailbox's sync. The
-- worker owns the live copy and relays every change as the SYNC_STATE job
-- event; the consumer writes it here and the loader hands it back when the
-- mailbox is (re)assigned, so a restarted or replaced worker resumes the
-- backfill where the previous one stopped instead of re-walking the mailbox.
--
-- backfill_cursor is jsonb on purpose: it is provider-shaped, opaque,
-- read-then-execute state (a Gmail page token, per-folder Graph nextLinks,
-- per-UIDVALIDITY IMAP floors) that is never filtered in SQL. It is kept
-- type-safe at the app boundary by models.SyncCursor.
CREATE TABLE public.email_sync_state (
    email_id uuid PRIMARY KEY REFERENCES public.email_accounts (id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    backfill_status text NOT NULL DEFAULT 'pending'
        CHECK (backfill_status IN ('pending', 'running', 'complete')),
    backfill_cursor jsonb NOT NULL DEFAULT '{}'::jsonb,
    backfill_synced integer NOT NULL DEFAULT 0,
    backfill_since timestamp with time zone,
    backfill_started_at timestamp with time zone,
    backfill_completed_at timestamp with time zone,
    throttled_until timestamp with time zone,
    throttle_reason text NOT NULL DEFAULT '',
    deferred integer NOT NULL DEFAULT 0,
    last_synced_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

-- The governor's priority lane asks "is this a reply to something this
-- mailbox sent?" once per new message. tasks.message_id had no index, so
-- that lookup (already used by reply detection) was a sequential scan.
CREATE INDEX IF NOT EXISTS idx_tasks_message_id ON public.tasks USING btree (message_id)
    WHERE message_id <> '';
