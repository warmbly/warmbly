-- Reverses 000152. The standing of every address is forgotten, so a penalised
-- address rejoins clean on its next connection, which is what happened before
-- the mirror existed. Current pool rows keep their state; only the copy that
-- outlives them goes.
DROP TRIGGER IF EXISTS warmup_reputation_mirror ON public.warmup_pool_participants;
DROP FUNCTION IF EXISTS public.warmup_reputation_mirror();
DROP TABLE IF EXISTS public.warmup_reputation_ledger;
