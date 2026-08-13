-- Registration attaches every new organization to the free-trial plan
-- (internal/app/trial.FreePlanID). Nothing but the dev seeder ever created that
-- row, so on a bare self-host install the first signup hit a foreign-key
-- violation on subscriptions.plan_id and the org ended up with no subscription
-- at all. Ship the row with the schema so signup works without seeding.
--
-- Limits here only bind Stripe deployments: BILLING_PROVIDER=none unlocks every
-- gate (internal/app/feature.gate selfHost), which is the self-host default.

INSERT INTO durations (id, title)
VALUES ('00000000-0000-0000-0000-0000000000d1', 'month')
ON CONFLICT (id) DO NOTHING;

INSERT INTO plans (
    id, name, max_contacts, daily_emails, ai_generation, account_limit,
    price, discounted_price, duration_id, savings, public,
    dedicated_workers, daily_campaign_limit,
    max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
    monthly_credits
) VALUES (
    '00000000-0000-0000-0000-000000000001', 'Free Trial', 100, 20, false, 2,
    0, 0, '00000000-0000-0000-0000-0000000000d1', 0, false,
    0, 20,
    2, 1, 1, 2,
    50
)
ON CONFLICT (id) DO NOTHING;
