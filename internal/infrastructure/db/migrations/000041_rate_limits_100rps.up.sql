-- Raise per-user and per-plan API rate limits so paid customers can sustain
-- ~100 req/s without bumping into category caps. 100 req/s = 6000 req/min.
-- Existing rows are bumped only when they still match the original defaults
-- so any admin-customized values stay intact.

ALTER TABLE user_rate_limits ALTER COLUMN limit_read_pm SET DEFAULT 6000;
ALTER TABLE user_rate_limits ALTER COLUMN limit_write_pm SET DEFAULT 6000;
ALTER TABLE user_rate_limits ALTER COLUMN limit_bulk_pm SET DEFAULT 600;
ALTER TABLE user_rate_limits ALTER COLUMN limit_unibox_pm SET DEFAULT 1200;
ALTER TABLE user_rate_limits ALTER COLUMN limit_analytics_pm SET DEFAULT 600;
ALTER TABLE user_rate_limits ALTER COLUMN limit_api_calls_daily SET DEFAULT 500000;
ALTER TABLE user_rate_limits ALTER COLUMN burst_multiplier SET DEFAULT 1.0;

ALTER TABLE plan_rate_limits ALTER COLUMN limit_read_pm SET DEFAULT 6000;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_write_pm SET DEFAULT 6000;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_bulk_pm SET DEFAULT 600;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_unibox_pm SET DEFAULT 1200;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_analytics_pm SET DEFAULT 600;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_api_calls_daily SET DEFAULT 500000;
ALTER TABLE plan_rate_limits ALTER COLUMN burst_multiplier SET DEFAULT 1.0;

-- Bump rows that still hold the pre-existing defaults. Anything custom is
-- left alone so admin-tuned accounts keep their overrides.
UPDATE user_rate_limits
SET limit_read_pm = 6000,
    limit_write_pm = 6000,
    limit_bulk_pm = 600,
    limit_unibox_pm = 1200,
    limit_analytics_pm = 600,
    limit_api_calls_daily = 500000,
    burst_multiplier = 1.0,
    updated_at = NOW()
WHERE limit_read_pm = 300
  AND limit_write_pm = 60
  AND limit_bulk_pm = 10
  AND limit_unibox_pm = 120
  AND limit_analytics_pm = 60
  AND limit_api_calls_daily = 50000
  AND burst_multiplier = 1.5;

UPDATE plan_rate_limits
SET limit_read_pm = 6000,
    limit_write_pm = 6000,
    limit_bulk_pm = 600,
    limit_unibox_pm = 1200,
    limit_analytics_pm = 600,
    limit_api_calls_daily = 500000,
    burst_multiplier = 1.0,
    updated_at = NOW()
WHERE limit_read_pm = 300
  AND limit_write_pm = 60
  AND limit_bulk_pm = 10
  AND limit_unibox_pm = 120
  AND limit_analytics_pm = 60
  AND limit_api_calls_daily = 50000
  AND burst_multiplier = 1.5;
