ALTER TABLE org_ai_settings DROP COLUMN IF EXISTS member_limit_monthly;
DROP INDEX IF EXISTS idx_credit_txns_member_debits;

-- Clear the use-AI bit (15); results fit in the positive smallint range.
UPDATE organization_roles SET permissions = (permissions::int & 32767)::smallint;
UPDATE organization_members SET permissions = (permissions::int & 32767)::smallint;
