DROP INDEX IF EXISTS idx_plans_stripe_price_yearly;
ALTER TABLE plans DROP COLUMN IF EXISTS stripe_price_id_yearly;
