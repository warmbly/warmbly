-- Proof fixture for flows/agency.flow.ts: the seeded owner runs "Northwind Growth" on Agency Pro,
-- with the seeded workspace as its first client and this month's sends in the shared pool.
-- Re-runnable. The flow adds two more client workspaces through the API.
BEGIN;

INSERT INTO agencies (id, name, owner_user_id, plan_id, status, current_period_start, current_period_end)
VALUES ('a9e00000-0000-0000-0000-000000000001', 'Northwind Growth', '11111111-0000-0000-0000-000000000001',
        '00000000-0000-0000-0000-0000000000a2', 'active',
        date_trunc('month', now()), date_trunc('month', now()) + interval '1 month')
ON CONFLICT (id) DO UPDATE SET plan_id = EXCLUDED.plan_id, status = 'active',
    current_period_start = EXCLUDED.current_period_start, current_period_end = EXCLUDED.current_period_end,
    cancel_at_period_end = false;

INSERT INTO agency_members (agency_id, user_id, role)
VALUES ('a9e00000-0000-0000-0000-000000000001', '11111111-0000-0000-0000-000000000001', 'owner')
ON CONFLICT (agency_id, user_id) DO NOTHING;

INSERT INTO agency_clients (organization_id, agency_id, added_by)
VALUES ('22222222-0000-0000-0000-000000000001', 'a9e00000-0000-0000-0000-000000000001', '11111111-0000-0000-0000-000000000001')
ON CONFLICT (organization_id) DO NOTHING;

-- The managed grant the agency service writes when it attaches a workspace.
UPDATE subscriptions SET managed_at = now(), managed_by = NULL, managed_reason = 'Billed through an agency',
    managed_plan_id = '00000000-0000-0000-0000-0000000000a2',
    managed_until = date_trunc('month', now()) + interval '1 month' + interval '3 days',
    managed_by_agency_id = 'a9e00000-0000-0000-0000-000000000001'
WHERE organization_id = '22222222-0000-0000-0000-000000000001';

INSERT INTO agency_pool_usage_days (agency_id, organization_id, day, sends)
SELECT 'a9e00000-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001', d::date,
       CASE WHEN extract(isodow FROM d) >= 6 THEN 600 ELSE 3200 + (extract(day FROM d)::int * 137) % 1400 END
FROM generate_series(date_trunc('month', now()), date_trunc('day', now()), interval '1 day') AS d
ON CONFLICT (agency_id, organization_id, day) DO UPDATE SET sends = EXCLUDED.sends;

-- The product tour would otherwise cover the page.
UPDATE users SET product_tour_completed_at = coalesce(product_tour_completed_at, now())
WHERE id = '11111111-0000-0000-0000-000000000001';

COMMIT;
