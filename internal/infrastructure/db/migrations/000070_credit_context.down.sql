ALTER TABLE credit_ledger_transactions
    DROP COLUMN IF EXISTS actor_user_id,
    DROP COLUMN IF EXISTS context;
