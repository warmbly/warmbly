-- The dev workspace on Starter, paid through a Stripe test customer that has a saved card, with
-- some of this month's included usage spent and money added earlier.
-- Apply with: psql -v customer=cus_... -f balance.sql (a customer in the same test account as
-- QA_STRIPE_SECRET_KEY, with a default card such as pm_card_visa).

-- The product tour would cover the page.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;

DELETE FROM subscriptions
WHERE organization_id = '22222222-0000-0000-0000-000000000001'
  AND id <> '10b40000-0000-0000-0000-000000000001';

INSERT INTO subscriptions (
    id, user_id, organization_id, plan_id,
    stripe_customer_id, stripe_subscription_id, stripe_price_id,
    status, current_period_start, current_period_end, cancel_at_period_end,
    is_enterprise, created_at, updated_at
) VALUES (
    '10b40000-0000-0000-0000-000000000001', '11111111-0000-0000-0000-000000000001',
    '22222222-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000110',
    :'customer', 'sub_qa_balance', 'price_qa_starter',
    'active', now() - interval '12 days', now() + interval '18 days', false,
    false, now(), now()
)
ON CONFLICT (id) DO UPDATE SET
    stripe_customer_id = EXCLUDED.stripe_customer_id,
    status = 'active',
    current_period_start = EXCLUDED.current_period_start,
    current_period_end = EXCLUDED.current_period_end,
    updated_at = now();

-- $2.44 of the $5 included left after this month's usage, $15 added earlier. The ledger's balance
-- is the included pool; added money is kept apart.
INSERT INTO credit_ledger (org_id, balance_micro, purchased_micro, total_purchased_micro, month_reset_at)
VALUES ('22222222-0000-0000-0000-000000000001', 2441200, 15000000, 15000000, now() - interval '12 days')
ON CONFLICT (org_id) DO UPDATE SET
    balance_micro = EXCLUDED.balance_micro,
    purchased_micro = EXCLUDED.purchased_micro,
    total_purchased_micro = EXCLUDED.total_purchased_micro,
    month_reset_at = EXCLUDED.month_reset_at,
    updated_at = now();

-- Charges priced at the default rates plus the 25% markup, so each one matches its tokens.
DELETE FROM credit_ledger_transactions WHERE org_id = '22222222-0000-0000-0000-000000000001';
INSERT INTO credit_ledger_transactions
    (id, org_id, reason, model_used, input_tokens, output_tokens, quantity, cost_micro,
     amount_micro, balance_after_micro, purchased_delta_micro, purchased_balance_after_micro, created_at)
VALUES
    ('10b40000-0000-0000-0001-000000000001', '22222222-0000-0000-0000-000000000001', 'balance_top_up', '', 0, 0, 0, 0,
     15000000, 0, 15000000, 15000000, now() - interval '19 days'),
    ('10b40000-0000-0000-0001-000000000002', '22222222-0000-0000-0000-000000000001', 'monthly_reset', '', 0, 0, 0, 0,
     5000000, 5000000, 0, 15000000, now() - interval '12 days'),
    ('10b40000-0000-0000-0001-000000000003', '22222222-0000-0000-0000-000000000001', 'research_run', 'claude-sonnet-4', 120000, 18000, 0, 630000,
     -787500, 4212500, 0, 15000000, now() - interval '10 days'),
    ('10b40000-0000-0000-0001-000000000004', '22222222-0000-0000-0000-000000000001', 'writing_assistant', 'gpt-6-luna', 2400, 600, 0, 540,
     -675, 4211825, 0, 15000000, now() - interval '8 days'),
    ('10b40000-0000-0000-0001-000000000005', '22222222-0000-0000-0000-000000000001', 'agent_iteration', 'claude-haiku-4', 40000, 6000, 0, 70000,
     -87500, 4124325, 0, 15000000, now() - interval '6 days'),
    ('10b40000-0000-0000-0001-000000000006', '22222222-0000-0000-0000-000000000001', 'placement_test', '', 0, 0, 1, 400000,
     -500000, 3624325, 0, 15000000, now() - interval '4 days'),
    ('10b40000-0000-0000-0001-000000000007', '22222222-0000-0000-0000-000000000001', 'inbox_agent_draft', 'claude-haiku-4', 9000, 1500, 0, 16500,
     -20625, 3603700, 0, 15000000, now() - interval '2 days'),
    ('10b40000-0000-0000-0001-000000000008', '22222222-0000-0000-0000-000000000001', 'research_run', 'claude-sonnet-4', 160000, 30000, 0, 930000,
     -1162500, 2441200, 0, 15000000, now() - interval '11 hours');

DELETE FROM org_ai_settings WHERE org_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM credit_auto_topup_attempts WHERE organization_id = '22222222-0000-0000-0000-000000000001';
