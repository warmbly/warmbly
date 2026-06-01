ALTER TABLE warmup_pool_participants
ADD COLUMN IF NOT EXISTS participant_role TEXT NOT NULL DEFAULT 'sender_receiver';

ALTER TABLE warmup_pool_participants
DROP CONSTRAINT IF EXISTS warmup_pool_participants_role_check;

ALTER TABLE warmup_pool_participants
ADD CONSTRAINT warmup_pool_participants_role_check
CHECK (participant_role IN ('sender_receiver', 'recipient_only'));

CREATE INDEX IF NOT EXISTS idx_warmup_participants_pool_role_health
ON warmup_pool_participants(pool_id, participant_role, health_state, blocked_until);
