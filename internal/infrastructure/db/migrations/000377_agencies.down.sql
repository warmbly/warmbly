-- Refuse rather than discard paid agency state.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agency_pool_topups) OR EXISTS (SELECT 1 FROM agencies WHERE stripe_subscription_id IS NOT NULL) THEN
        RAISE EXCEPTION 'agencies hold billing records written after 000377; roll forward instead of downgrading past it';
    END IF;
END $$;

-- An agency grant ends with the agency; plan_id was never touched by it.
UPDATE subscriptions
SET managed_at = NULL, managed_by = NULL, managed_reason = NULL, managed_until = NULL, managed_plan_id = NULL, updated_at = now()
WHERE managed_by_agency_id IS NOT NULL;

DROP INDEX IF EXISTS idx_organization_members_via_agency;
ALTER TABLE organization_members DROP COLUMN IF EXISTS via_agency_id;
DROP INDEX IF EXISTS idx_subscriptions_managed_by_agency;
ALTER TABLE subscriptions DROP COLUMN IF EXISTS managed_by_agency_id;

DROP TABLE IF EXISTS agency_pool_usage_days;
DROP TABLE IF EXISTS agency_pool_topups;
DROP TABLE IF EXISTS agency_clients;
DROP TABLE IF EXISTS agency_invitations;
DROP TABLE IF EXISTS agency_members;
DROP TABLE IF EXISTS agencies;

DELETE FROM plans p
WHERE p.id IN ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-0000000000a2')
  AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.plan_id = p.id OR s.managed_plan_id = p.id);
ALTER TABLE plans DROP COLUMN IF EXISTS monthly_send_pool;
