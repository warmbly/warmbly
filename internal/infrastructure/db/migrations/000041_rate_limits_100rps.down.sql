ALTER TABLE user_rate_limits ALTER COLUMN limit_read_pm SET DEFAULT 300;
ALTER TABLE user_rate_limits ALTER COLUMN limit_write_pm SET DEFAULT 60;
ALTER TABLE user_rate_limits ALTER COLUMN limit_bulk_pm SET DEFAULT 10;
ALTER TABLE user_rate_limits ALTER COLUMN limit_unibox_pm SET DEFAULT 120;
ALTER TABLE user_rate_limits ALTER COLUMN limit_analytics_pm SET DEFAULT 60;
ALTER TABLE user_rate_limits ALTER COLUMN limit_api_calls_daily SET DEFAULT 50000;
ALTER TABLE user_rate_limits ALTER COLUMN burst_multiplier SET DEFAULT 1.5;

ALTER TABLE plan_rate_limits ALTER COLUMN limit_read_pm SET DEFAULT 300;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_write_pm SET DEFAULT 60;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_bulk_pm SET DEFAULT 10;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_unibox_pm SET DEFAULT 120;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_analytics_pm SET DEFAULT 60;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_analytics_pm SET DEFAULT 60;
ALTER TABLE plan_rate_limits ALTER COLUMN limit_api_calls_daily SET DEFAULT 50000;
ALTER TABLE plan_rate_limits ALTER COLUMN burst_multiplier SET DEFAULT 1.5;
