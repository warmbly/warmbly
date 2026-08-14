-- 000089_confenge_activation_priority.down.sql

DROP TABLE IF EXISTS outreach_feed_sync_state;

ALTER TABLE outreach_touchpoints
    DROP COLUMN IF EXISTS generated_context_hash;

DROP INDEX IF EXISTS outreach_accounts_org_activation_score_idx;
DROP INDEX IF EXISTS outreach_accounts_org_nba_idx;
DROP INDEX IF EXISTS outreach_accounts_org_activation_state_idx;

ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_activation_score_check;
ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_activation_state_check;

ALTER TABLE outreach_accounts
    DROP COLUMN IF EXISTS score_components,
    DROP COLUMN IF EXISTS message_context_hash,
    DROP COLUMN IF EXISTS activation_source_hash,
    DROP COLUMN IF EXISTS activation_expires_at,
    DROP COLUMN IF EXISTS next_best_action_at,
    DROP COLUMN IF EXISTS activation_evaluated_at,
    DROP COLUMN IF EXISTS activation_policy_version,
    DROP COLUMN IF EXISTS activation_reason_codes,
    DROP COLUMN IF EXISTS activation_score,
    DROP COLUMN IF EXISTS activation_state;
