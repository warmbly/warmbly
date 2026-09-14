-- One pool per type, under fixed ids, on every instance. The baseline squash
-- (d92e7f1b) dropped the insert the original 000010 carried, so a fresh instance
-- had no pools and failed its first warmup tick on "warmup pool not found".

-- 1. The canonical rows exist from here on.
INSERT INTO public.warmup_pools (id, pool_type, name, description)
VALUES ('77777777-aaaa-0000-0000-000000000001', 'free', 'Free warmup pool', 'Created at install'),
       ('77777777-aaaa-0000-0000-000000000002', 'premium', 'Premium warmup pool', 'Created at install')
ON CONFLICT (id) DO NOTHING;

-- 2. Every membership moves onto the canonical pool of its type. The mirror
--    trigger (000152) is off for it: the pool is not part of a standing, and
--    re-mirroring would restart every penalised address's retention window.
ALTER TABLE public.warmup_pool_participants DISABLE TRIGGER warmup_reputation_mirror;
UPDATE public.warmup_pool_participants p
   SET pool_id = CASE w.pool_type
                     WHEN 'free' THEN '77777777-aaaa-0000-0000-000000000001'::uuid
                     ELSE '77777777-aaaa-0000-0000-000000000002'::uuid
                 END
  FROM public.warmup_pools w
 WHERE w.id = p.pool_id
   AND w.pool_type IN ('free', 'premium')
   AND w.id NOT IN ('77777777-aaaa-0000-0000-000000000001', '77777777-aaaa-0000-0000-000000000002');
ALTER TABLE public.warmup_pool_participants ENABLE TRIGGER warmup_reputation_mirror;

-- 3. Every other pool of those types goes; nothing references it any more.
DELETE FROM public.warmup_pools
 WHERE pool_type IN ('free', 'premium')
   AND id NOT IN ('77777777-aaaa-0000-0000-000000000001', '77777777-aaaa-0000-0000-000000000002');

-- 4. One pool per type, structurally.
CREATE UNIQUE INDEX warmup_pools_pool_type_key ON public.warmup_pools (pool_type);
