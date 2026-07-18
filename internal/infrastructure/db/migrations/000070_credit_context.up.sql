-- Full attribution on the credit trail: who triggered a charge (null for
-- scheduled/system work) and a structured context blob naming exactly what ran
-- (campaign/step/contact, automation/node/run, thread, session). Context is
-- jsonb by design: a free-form, evolving, read-then-display blob that is not
-- filtered in SQL; the app boundary keeps it typed (models.CreditContext).
ALTER TABLE credit_ledger_transactions
    ADD COLUMN IF NOT EXISTS actor_user_id UUID,
    ADD COLUMN IF NOT EXISTS context JSONB NOT NULL DEFAULT '{}'::jsonb;
