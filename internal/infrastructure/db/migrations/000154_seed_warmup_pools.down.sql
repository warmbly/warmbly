-- Reverses 000154: the uniqueness goes. The pools stay, since memberships
-- reference them and deleting them would cascade every membership away.
DROP INDEX IF EXISTS public.warmup_pools_pool_type_key;
