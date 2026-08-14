-- 000089_confenge_activation_priority.up.sql
-- Additive activation planner projection on outreach_accounts + message context hash
-- for stale-approval invalidation. Activation is external intelligence (extra-cli);
-- queue_state remains local execution readiness. Not a second commercial scoring engine.

ALTER TABLE outreach_accounts
    ADD COLUMN IF NOT EXISTS activation_state text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS activation_score double precision NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS activation_reason_codes jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS activation_policy_version text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS activation_evaluated_at timestamptz,
    ADD COLUMN IF NOT EXISTS next_best_action_at timestamptz,
    ADD COLUMN IF NOT EXISTS activation_expires_at timestamptz,
    ADD COLUMN IF NOT EXISTS activation_source_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS message_context_hash text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS score_components jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_activation_state_check;

ALTER TABLE outreach_accounts
    ADD CONSTRAINT outreach_accounts_activation_state_check
        CHECK (activation_state IN (
            '', 'WATCH', 'RESEARCH_REQUIRED', 'ACTIONABLE_NOW', 'SUPPRESSED'
        ));

ALTER TABLE outreach_accounts
    DROP CONSTRAINT IF EXISTS outreach_accounts_activation_score_check;

ALTER TABLE outreach_accounts
    ADD CONSTRAINT outreach_accounts_activation_score_check
        CHECK (activation_score >= 0 AND activation_score <= 100);

CREATE INDEX IF NOT EXISTS outreach_accounts_org_activation_state_idx
    ON outreach_accounts (organization_id, activation_state);

CREATE INDEX IF NOT EXISTS outreach_accounts_org_nba_idx
    ON outreach_accounts (organization_id, next_best_action_at)
    WHERE next_best_action_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS outreach_accounts_org_activation_score_idx
    ON outreach_accounts (organization_id, activation_score DESC)
    WHERE activation_state = 'ACTIONABLE_NOW';

-- Capture context hash at generation time for stale-approval guard.
ALTER TABLE outreach_touchpoints
    ADD COLUMN IF NOT EXISTS generated_context_hash text NOT NULL DEFAULT '';

-- Feed sync progress (manifest-level, single-flight via advisory lock in app).
CREATE TABLE IF NOT EXISTS outreach_feed_sync_state (
    organization_id     uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    last_snapshot_hash  text NOT NULL DEFAULT '',
    last_run_id         text NOT NULL DEFAULT '',
    last_manifest_uri   text NOT NULL DEFAULT '',
    last_success_at     timestamptz,
    last_attempt_at     timestamptz,
    last_error          text NOT NULL DEFAULT '',
    last_status         text NOT NULL DEFAULT 'idle',
    counts              jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outreach_feed_sync_status_check
        CHECK (last_status IN ('idle', 'running', 'completed', 'failed', 'partial'))
);

COMMENT ON COLUMN outreach_accounts.activation_state IS
'External commercial activation projection from extra-cli. Not queue_state.';
COMMENT ON COLUMN outreach_accounts.message_context_hash IS
'Hash of message-material fields only (moment/offer/messaging/evidence/recipient). Rank/score changes do not update this.';
COMMENT ON COLUMN outreach_touchpoints.generated_context_hash IS
'message_context_hash captured when draft content was generated; must match account at dispatch.';
