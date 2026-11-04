BEGIN;
ALTER TABLE subscriptions DROP COLUMN keeps_free_warmup;
COMMIT;
