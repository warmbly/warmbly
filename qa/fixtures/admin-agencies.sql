-- Proof fixture for flows/admin-agencies.flow.ts: the seeded account holds every admin permission
-- with a session marked as having passed a second factor (a local authentication fixture, not MFA
-- proof), and "Meridian Outbound" has the seeded workspace as a client and no plan yet. Re-runnable.
BEGIN;

-- The product tour is marked seen, so it does not open over a walkthrough on a fresh database.
UPDATE users SET admin_permissions = 8388607, product_tour_completed_at = COALESCE(product_tour_completed_at, now())
WHERE id = '11111111-0000-0000-0000-000000000001';
UPDATE sessions SET mfa_verified = true WHERE user_id = '11111111-0000-0000-0000-000000000001';

INSERT INTO agencies (id, name, owner_user_id, status)
VALUES ('a9e00000-0000-0000-0000-000000000002', 'Meridian Outbound', '11111111-0000-0000-0000-000000000001', 'incomplete')
ON CONFLICT (id) DO UPDATE SET plan_id = NULL, status = 'incomplete', current_period_start = NULL, current_period_end = NULL,
    managed_plan_id = NULL, managed_at = NULL, managed_by = NULL, managed_reason = NULL, managed_until = NULL,
    pool_override = NULL, ai_allowance_override_micro = NULL, override_reason = NULL, override_by = NULL, override_at = NULL;

INSERT INTO agency_members (agency_id, user_id, role)
VALUES ('a9e00000-0000-0000-0000-000000000002', '11111111-0000-0000-0000-000000000001', 'owner')
ON CONFLICT (agency_id, user_id) DO NOTHING;

INSERT INTO agency_clients (organization_id, agency_id, added_by)
VALUES ('22222222-0000-0000-0000-000000000001', 'a9e00000-0000-0000-0000-000000000002', '11111111-0000-0000-0000-000000000001')
ON CONFLICT (organization_id) DO UPDATE SET agency_id = EXCLUDED.agency_id;

-- The agency holds no inbox vendor account, so flows/agency-mailboxes.flow.ts connects its first.
DELETE FROM agency_vendor_connections WHERE agency_id = 'a9e00000-0000-0000-0000-000000000002';
DELETE FROM agency_pool_topups WHERE agency_id = 'a9e00000-0000-0000-0000-000000000002';
-- No brand yet, so the branding page opens as a new agency sees it.
DELETE FROM agency_brands WHERE agency_id = 'a9e00000-0000-0000-0000-000000000002';
DELETE FROM agency_ai_transactions WHERE agency_id = 'a9e00000-0000-0000-0000-000000000002';
DELETE FROM agency_ai_ledger WHERE agency_id = 'a9e00000-0000-0000-0000-000000000002';
UPDATE subscriptions SET managed_at = NULL, managed_by = NULL, managed_reason = NULL, managed_until = NULL,
    managed_plan_id = NULL, managed_by_agency_id = NULL
WHERE managed_by_agency_id = 'a9e00000-0000-0000-0000-000000000002';

-- This month's sends, so the pool the operator sets has something drawn from it.
INSERT INTO agency_pool_usage_days (agency_id, organization_id, day, sends)
SELECT 'a9e00000-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001', d::date,
       CASE WHEN extract(isodow FROM d) >= 6 THEN 4000 ELSE 21000 + (extract(day FROM d)::int * 911) % 9000 END
FROM generate_series(date_trunc('month', now()), date_trunc('day', now()), interval '1 day') AS d
ON CONFLICT (agency_id, organization_id, day) DO UPDATE SET sends = EXCLUDED.sends;

COMMIT;
