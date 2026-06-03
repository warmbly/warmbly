-- Realign the content-pick index with the shared-library selection predicate.
--
-- PickConversation no longer filters by pool_type (content is one shared library;
-- pools only isolate mailbox reputation). It now selects:
--   WHERE status = 'active' AND (segment = $1 OR segment = '')
-- so the old idx_warmup_conversations_pick (pool_type, segment, status) — which
-- leads on the unused pool_type column — no longer matches. Replace it with a
-- partial index on the columns actually filtered.

DROP INDEX IF EXISTS public.idx_warmup_conversations_pick;

CREATE INDEX idx_warmup_conversations_pick ON public.warmup_conversations
    USING btree (segment) WHERE (status = 'active'::text);
