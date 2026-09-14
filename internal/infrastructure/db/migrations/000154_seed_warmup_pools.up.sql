-- Nothing created the warmup pools: a fresh instance had none, so its first
-- warmup tick failed on "warmup pool not found" and warmup never started. One
-- pool per type is what every reader assumes, so make it structural and seed
-- the two pools under the ids the sandbox and the live tests already use.
UPDATE public.warmup_pool_participants p
   SET pool_id = k.id
  FROM public.warmup_pools w
  JOIN (SELECT DISTINCT ON (pool_type) id, pool_type
          FROM public.warmup_pools
         ORDER BY pool_type, created_at, id) k ON k.pool_type = w.pool_type
 WHERE p.pool_id = w.id AND w.id <> k.id;

DELETE FROM public.warmup_pools w
 USING (SELECT DISTINCT ON (pool_type) id, pool_type
          FROM public.warmup_pools
         ORDER BY pool_type, created_at, id) k
 WHERE k.pool_type = w.pool_type AND w.id <> k.id;

CREATE UNIQUE INDEX warmup_pools_pool_type_key ON public.warmup_pools (pool_type);

INSERT INTO public.warmup_pools (id, pool_type, name, description)
VALUES ('77777777-aaaa-0000-0000-000000000001', 'free', 'Free warmup pool', 'Created at install'),
       ('77777777-aaaa-0000-0000-000000000002', 'premium', 'Premium warmup pool', 'Created at install')
ON CONFLICT (pool_type) DO NOTHING;
