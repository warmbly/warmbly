-- Seed the global warmup pools required by warmup membership and partner selection.
-- The baseline schema creates warmup_pools but does not populate it; without
-- these rows, EnsurePoolMembershipWithRole returns "warmup pool not found" and
-- onboarding/start-warmup cannot join accounts to the free/premium pools.
INSERT INTO warmup_pools (id, pool_type, name, description, max_participants)
SELECT '77777777-aaaa-0000-0000-000000000001'::uuid,
       'free'::warmup_pool_type,
       'Free warmup pool',
       'Default free-tier warmup pool',
       1000
WHERE NOT EXISTS (SELECT 1 FROM warmup_pools WHERE pool_type = 'free'::warmup_pool_type);

INSERT INTO warmup_pools (id, pool_type, name, description, max_participants)
SELECT '77777777-aaaa-0000-0000-000000000002'::uuid,
       'premium'::warmup_pool_type,
       'Premium warmup pool',
       'Default paid-tier warmup pool',
       1000
WHERE NOT EXISTS (SELECT 1 FROM warmup_pools WHERE pool_type = 'premium'::warmup_pool_type);
