-- Revert: remove 'throttled' from the warmup health state check constraint
-- Note: any rows with health_state='throttled' must be updated first
UPDATE warmup_pool_participants SET health_state = 'watch' WHERE health_state = 'throttled';

ALTER TABLE warmup_pool_participants
DROP CONSTRAINT IF EXISTS warmup_pool_participants_health_state_check;

ALTER TABLE warmup_pool_participants
ADD CONSTRAINT warmup_pool_participants_health_state_check
CHECK (health_state IN ('healthy', 'watch', 'quarantined', 'blocked'));
