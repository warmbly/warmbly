BEGIN;
-- Every workspace that exists now keeps the free warmup allowance; new ones start without it.
ALTER TABLE subscriptions ADD COLUMN keeps_free_warmup boolean NOT NULL DEFAULT true;
ALTER TABLE subscriptions ALTER COLUMN keeps_free_warmup SET DEFAULT false;
COMMIT;
