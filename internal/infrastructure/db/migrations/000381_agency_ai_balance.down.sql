-- Refuse rather than discard money an agency paid for.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM agency_ai_transactions WHERE kind = 'topup') THEN
        RAISE EXCEPTION 'agencies hold AI balance purchases written after 000381; roll forward instead of downgrading past it';
    END IF;
END $$;

DROP TABLE IF EXISTS agency_ai_transactions;
DROP TABLE IF EXISTS agency_ai_ledger;
ALTER TABLE credit_ledger_transactions DROP COLUMN IF EXISTS agency_delta_micro;
ALTER TABLE agency_clients DROP COLUMN IF EXISTS monthly_ai_limit_micro;
ALTER TABLE plans DROP COLUMN IF EXISTS agency_ai_allowance_micro;
