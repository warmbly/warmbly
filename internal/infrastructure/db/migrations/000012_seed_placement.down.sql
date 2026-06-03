DROP TABLE IF EXISTS placement_results;
DROP TABLE IF EXISTS placement_tests;

DROP INDEX IF EXISTS idx_email_accounts_is_seed;
ALTER TABLE email_accounts DROP COLUMN IF EXISTS is_seed;
