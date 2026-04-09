-- Add 'throttled' to the warmup health state check constraint
ALTER TABLE warmup_pool_participants
DROP CONSTRAINT IF EXISTS warmup_pool_participants_health_state_check;

ALTER TABLE warmup_pool_participants
ADD CONSTRAINT warmup_pool_participants_health_state_check
CHECK (health_state IN ('healthy', 'watch', 'throttled', 'quarantined', 'blocked'));
