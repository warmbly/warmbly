-- Reverses 000150. The standing of every removed mailbox is forgotten, so an
-- address removed while penalised rejoins clean on its next connection, which
-- is what happened before the ledger existed. Nothing else is touched: current
-- pool rows keep their state, and MoveToPool tolerates the table's absence only
-- at the version this migration is paired with.
DROP TABLE IF EXISTS public.warmup_reputation_ledger;
