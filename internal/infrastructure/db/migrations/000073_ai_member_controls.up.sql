-- Per-member AI governance on top of the org-wide spend limits: an optional
-- monthly credit ceiling that applies to each member individually, enforced
-- against the ledger's actor attribution (credit_ledger_transactions.actor_user_id).
ALTER TABLE org_ai_settings
    ADD COLUMN IF NOT EXISTS member_limit_monthly INTEGER CHECK (member_limit_monthly IS NULL OR member_limit_monthly > 0);

-- Member-scoped debit sums for the per-member limit check.
CREATE INDEX IF NOT EXISTS idx_credit_txns_member_debits
    ON credit_ledger_transactions (org_id, actor_user_id, created_at DESC)
    WHERE amount < 0 AND actor_user_id IS NOT NULL;

-- Use-AI organization permission (bit 15 of the member permission mask).
-- Backfill it onto every existing role and member override so nobody loses
-- assistant access on deploy; new orgs get it via the seeded roles. The
-- column is a signed SMALLINT holding uint16 bits, hence the reinterpret
-- dance: setting bit 15 always lands in [32768, 65535], stored as value-65536.
UPDATE organization_roles
SET permissions = (((permissions::int & 65535) | 32768) - 65536)::smallint;
UPDATE organization_members
SET permissions = (((permissions::int & 65535) | 32768) - 65536)::smallint;
