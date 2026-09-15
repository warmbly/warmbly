-- The up migration only queues domains for re-checking and documents a column.
-- Un-queueing them would mean inventing a check time that never happened, so
-- the down migration drops the comment and leaves the sweep alone.
COMMENT ON COLUMN email_accounts.auth_dkim IS NULL;
