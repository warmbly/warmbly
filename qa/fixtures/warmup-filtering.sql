-- The product tour would cover the page; the flow records warmup filtering, not the tour.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;
-- Start from the defaults so the walkthrough shows both settings turning on.
UPDATE outreach_settings SET settings = settings - 'external_warmup'
WHERE organization_id IN (SELECT organization_id FROM users u JOIN organization_members m ON m.user_id = u.id WHERE u.id = '11111111-0000-0000-0000-000000000001');
UPDATE email_accounts SET warmup_filter_tag = NULL WHERE email = 'dev.outbound@warmbly.test';
