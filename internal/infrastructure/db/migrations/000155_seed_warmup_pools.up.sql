-- One pool per type, under fixed ids, on every instance. The baseline squash
-- (d92e7f1b) dropped the insert the original 000010 carried, so a fresh instance
-- had no pools and failed its first warmup tick on "warmup pool not found".

-- 1. The canonical rows exist from here on.
INSERT INTO public.warmup_pools (id, pool_type, name, description)
VALUES ('77777777-aaaa-0000-0000-000000000001', 'free', 'Free warmup pool', 'Created at install'),
       ('77777777-aaaa-0000-0000-000000000002', 'premium', 'Premium warmup pool', 'Created at install')
ON CONFLICT (id) DO NOTHING;

-- 2. The standing mirror (000152) fires on every UPDATE, so a pool move
--    restarted a penalised address's retention window. Scope it to the
--    columns it mirrors; pool_id is not one of them.
DROP TRIGGER IF EXISTS warmup_reputation_mirror ON public.warmup_pool_participants;
CREATE TRIGGER warmup_reputation_mirror
    AFTER INSERT OR UPDATE OF spam_score, health_state, blocked_at, blocked_until, blocked_reason,
                              last_health_score, last_health_reason
    ON public.warmup_pool_participants
    FOR EACH ROW EXECUTE FUNCTION public.warmup_reputation_mirror();

-- 3. Every membership moves onto the canonical pool of its type.
UPDATE public.warmup_pool_participants p
   SET pool_id = c.id
  FROM public.warmup_pools w
  JOIN public.warmup_pools c
    ON c.pool_type = w.pool_type
   AND c.id IN ('77777777-aaaa-0000-0000-000000000001', '77777777-aaaa-0000-0000-000000000002')
 WHERE w.id = p.pool_id AND w.id <> c.id;

-- 4. Every other pool goes; nothing references it any more.
DELETE FROM public.warmup_pools
 WHERE id NOT IN ('77777777-aaaa-0000-0000-000000000001', '77777777-aaaa-0000-0000-000000000002');

-- 5. One pool per type, structurally.
CREATE UNIQUE INDEX warmup_pools_pool_type_key ON public.warmup_pools (pool_type);
