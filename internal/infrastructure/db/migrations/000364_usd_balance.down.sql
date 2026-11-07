ALTER TABLE lead_enrichment_runs DROP COLUMN IF EXISTS charged_micro;
ALTER TABLE contact_research_runs DROP COLUMN IF EXISTS charged_micro;
ALTER TABLE placement_batches
    DROP COLUMN IF EXISTS max_micro,
    DROP COLUMN IF EXISTS spent_micro;
ALTER TABLE placement_tests
    DROP COLUMN IF EXISTS charged_micro,
    DROP COLUMN IF EXISTS refunded_micro;

DROP INDEX IF EXISTS credit_auto_topup_attempts_invoice;
ALTER TABLE credit_auto_topup_attempts
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS amount_micro,
    DROP COLUMN IF EXISTS amount_cents,
    DROP COLUMN IF EXISTS currency,
    DROP COLUMN IF EXISTS invoice_id,
    DROP COLUMN IF EXISTS customer_id,
    DROP COLUMN IF EXISTS payment_method_id,
    DROP COLUMN IF EXISTS tries,
    DROP COLUMN IF EXISTS retry_at,
    DROP COLUMN IF EXISTS failure_code;

DROP TRIGGER IF EXISTS org_ai_settings_usd_sync ON org_ai_settings;
DROP FUNCTION IF EXISTS org_ai_settings_usd_sync();
ALTER TABLE org_ai_settings
    DROP CONSTRAINT IF EXISTS org_ai_settings_auto_reload_shape,
    DROP COLUMN IF EXISTS spend_limit_daily_micro,
    DROP COLUMN IF EXISTS spend_limit_weekly_micro,
    DROP COLUMN IF EXISTS spend_limit_monthly_micro,
    DROP COLUMN IF EXISTS member_limit_daily_micro,
    DROP COLUMN IF EXISTS member_limit_weekly_micro,
    DROP COLUMN IF EXISTS member_limit_monthly_micro,
    DROP COLUMN IF EXISTS low_balance_threshold_micro,
    DROP COLUMN IF EXISTS auto_reload_enabled,
    DROP COLUMN IF EXISTS auto_reload_threshold_micro,
    DROP COLUMN IF EXISTS auto_reload_amount_micro,
    DROP COLUMN IF EXISTS auto_reload_monthly_limit_micro,
    DROP COLUMN IF EXISTS auto_reload_failure_code,
    DROP COLUMN IF EXISTS auto_reload_failed_at,
    DROP COLUMN IF EXISTS balance_out_notified_at;

ALTER TABLE plans DROP COLUMN IF EXISTS monthly_allowance_micro;

DROP TRIGGER IF EXISTS credit_txn_usd_sync ON credit_ledger_transactions;
DROP FUNCTION IF EXISTS credit_txn_usd_sync();
ALTER TABLE credit_ledger_transactions
    DROP COLUMN IF EXISTS amount_micro,
    DROP COLUMN IF EXISTS balance_after_micro,
    DROP COLUMN IF EXISTS purchased_delta_micro,
    DROP COLUMN IF EXISTS purchased_balance_after_micro,
    DROP COLUMN IF EXISTS input_tokens,
    DROP COLUMN IF EXISTS output_tokens,
    DROP COLUMN IF EXISTS quantity,
    DROP COLUMN IF EXISTS cost_micro;

DROP TRIGGER IF EXISTS credit_ledger_usd_sync ON credit_ledger;
DROP FUNCTION IF EXISTS credit_ledger_usd_sync();
ALTER TABLE credit_ledger
    DROP COLUMN IF EXISTS balance_micro,
    DROP COLUMN IF EXISTS purchased_micro,
    DROP COLUMN IF EXISTS total_purchased_micro;

DROP FUNCTION IF EXISTS micro_to_credit_limit(bigint);
DROP FUNCTION IF EXISTS micro_to_credits(bigint);
DROP FUNCTION IF EXISTS legacy_credit_micro();
