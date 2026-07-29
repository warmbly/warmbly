ALTER TABLE credit_ledger_transactions
    DROP COLUMN IF EXISTS purchased_balance_after,
    DROP COLUMN IF EXISTS purchased_delta;

ALTER TABLE credit_ledger
    DROP COLUMN IF EXISTS purchased_balance;
