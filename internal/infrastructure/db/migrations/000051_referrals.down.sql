DROP TABLE IF EXISTS referral_earnings_transactions;
DROP TABLE IF EXISTS referral_earnings_ledger;
DROP TABLE IF EXISTS referral_attributions;
DROP TABLE IF EXISTS referral_codes;
ALTER TABLE plans DROP COLUMN IF EXISTS referral_reward_percent;
