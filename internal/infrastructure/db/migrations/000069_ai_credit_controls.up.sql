-- Org-level AI spend controls: configurable day/week/month spend limits,
-- low-balance alerting state, and auto top-up configuration. One row per org,
-- created lazily on first save; absent row = defaults (no limits, alerts at
-- the service default threshold, auto top-up off).
CREATE TABLE IF NOT EXISTS org_ai_settings (
    org_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    spend_limit_daily INTEGER CHECK (spend_limit_daily IS NULL OR spend_limit_daily > 0),
    spend_limit_weekly INTEGER CHECK (spend_limit_weekly IS NULL OR spend_limit_weekly > 0),
    spend_limit_monthly INTEGER CHECK (spend_limit_monthly IS NULL OR spend_limit_monthly > 0),
    low_balance_threshold INTEGER NOT NULL DEFAULT 25 CHECK (low_balance_threshold >= 0),
    low_balance_notified_at TIMESTAMPTZ,
    auto_topup_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    auto_topup_pack TEXT NOT NULL DEFAULT 'pack_500',
    auto_topup_threshold INTEGER NOT NULL DEFAULT 50 CHECK (auto_topup_threshold >= 0),
    auto_topup_max_per_month INTEGER NOT NULL DEFAULT 2 CHECK (auto_topup_max_per_month >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Spend-window checks and the usage overview aggregate only debit rows; the
-- partial index keeps those per-consume queries cheap as the log grows.
CREATE INDEX IF NOT EXISTS idx_credit_txns_org_debits
    ON credit_ledger_transactions (org_id, created_at DESC)
    WHERE amount < 0;
