-- Drop the burst_multiplier knob from both rate-limit tables.
--
-- It was a ratio applied on top of the base per-minute limits, but nothing
-- needed the multiplicative shape: plan tiers and admin overrides express
-- the actual ceiling more clearly as a fixed limit_*_pm value. Removing the
-- column simplifies CheckLimit / CheckAndRecord — the base limit is now
-- the ceiling, full stop. Per-customer enterprise allowances still work by
-- raising limit_*_pm directly on user_rate_limits.

ALTER TABLE user_rate_limits DROP COLUMN burst_multiplier;
ALTER TABLE plan_rate_limits DROP COLUMN burst_multiplier;
