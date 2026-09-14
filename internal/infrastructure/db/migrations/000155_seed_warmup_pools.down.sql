-- Partly one-way. The index goes. The pools stay, since memberships reference
-- them, and the moves onto the canonical ids are not undone; running up again
-- is safe (ON CONFLICT (id) DO NOTHING). The narrowed trigger stays: it is a
-- fix, not a dependency of the index.
DROP INDEX IF EXISTS public.warmup_pools_pool_type_key;
