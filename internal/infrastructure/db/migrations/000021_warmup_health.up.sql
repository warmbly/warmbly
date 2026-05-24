ALTER TABLE warmup_pool_participants
ADD COLUMN IF NOT EXISTS blocked_until TIMESTAMPTZ,
ADD COLUMN IF NOT EXISTS health_state VARCHAR(20) NOT NULL DEFAULT 'healthy',
ADD COLUMN IF NOT EXISTS last_health_score DOUBLE PRECISION NOT NULL DEFAULT 0,
ADD COLUMN IF NOT EXISTS last_health_reason TEXT,
ADD COLUMN IF NOT EXISTS last_health_evaluated_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'warmup_pool_participants_health_state_check'
    ) THEN
        ALTER TABLE warmup_pool_participants
        ADD CONSTRAINT warmup_pool_participants_health_state_check
        CHECK (health_state IN ('healthy', 'watch', 'quarantined', 'blocked'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_warmup_participants_health_state
    ON warmup_pool_participants(health_state, blocked_until);

CREATE INDEX IF NOT EXISTS idx_warmup_participants_account_health
    ON warmup_pool_participants(email_account_id, health_state, blocked_until);
