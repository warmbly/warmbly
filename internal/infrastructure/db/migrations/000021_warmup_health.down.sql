DROP INDEX IF EXISTS idx_warmup_participants_account_health;
DROP INDEX IF EXISTS idx_warmup_participants_health_state;

ALTER TABLE warmup_pool_participants
DROP CONSTRAINT IF EXISTS warmup_pool_participants_health_state_check;

ALTER TABLE warmup_pool_participants
DROP COLUMN IF EXISTS last_health_evaluated_at,
DROP COLUMN IF EXISTS last_health_reason,
DROP COLUMN IF EXISTS last_health_score,
DROP COLUMN IF EXISTS health_state,
DROP COLUMN IF EXISTS blocked_until;
