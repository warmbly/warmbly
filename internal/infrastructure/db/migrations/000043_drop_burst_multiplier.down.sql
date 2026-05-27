ALTER TABLE user_rate_limits ADD COLUMN burst_multiplier DECIMAL(3, 2) NOT NULL DEFAULT 1.0;
ALTER TABLE plan_rate_limits ADD COLUMN burst_multiplier DECIMAL(3, 2) NOT NULL DEFAULT 1.0;
