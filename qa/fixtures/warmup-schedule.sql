BEGIN;

UPDATE email_accounts
SET warmup_days = 0, warmup_start_time = '08:00', warmup_end_time = '20:00'
WHERE organization_id = '22222222-0000-0000-0000-000000000001'
  AND email = 'dev.outbound@warmbly.test';

INSERT INTO email_account_behavior (email_account_id, enabled, weekdays, daily_limit_min, daily_limit_max)
SELECT id, true, 31, 28, 32 FROM email_accounts
WHERE organization_id = '22222222-0000-0000-0000-000000000001'
  AND email = 'dev.outbound@warmbly.test'
ON CONFLICT (email_account_id) DO UPDATE
SET enabled = true, weekdays = 31;

UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;

COMMIT;
