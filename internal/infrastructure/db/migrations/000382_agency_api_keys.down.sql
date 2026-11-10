-- Agency-scoped idempotency records are a 24-hour replay cache; dropping them loses no data.
DELETE FROM api_idempotency_keys WHERE organization_id IS NULL;
DROP INDEX IF EXISTS api_idempotency_keys_agency_key;
ALTER TABLE api_idempotency_keys DROP CONSTRAINT IF EXISTS api_idempotency_keys_one_scope;
ALTER TABLE api_idempotency_keys ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE api_idempotency_keys DROP COLUMN IF EXISTS agency_id;

DROP TABLE IF EXISTS agency_api_keys;
