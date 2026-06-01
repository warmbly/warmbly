DROP INDEX IF EXISTS idx_warmup_participants_pool_role_health;

ALTER TABLE warmup_pool_participants
DROP CONSTRAINT IF EXISTS warmup_pool_participants_role_check;

ALTER TABLE warmup_pool_participants
DROP COLUMN IF EXISTS participant_role;
