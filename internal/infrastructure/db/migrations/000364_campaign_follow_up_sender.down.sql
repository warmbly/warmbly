ALTER TABLE campaigns DROP CONSTRAINT IF EXISTS campaigns_follow_up_sender_check;
ALTER TABLE campaigns DROP COLUMN IF EXISTS follow_up_sender;
