-- Which mailbox a lead's follow-ups leave from. 'same' keeps every step on the
-- address that sent the first email, which is what every existing campaign
-- does; 'same_domain' rotates among mailboxes on that address's domain and
-- 'rotate' places each step like a first email.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS follow_up_sender text NOT NULL DEFAULT 'same';

ALTER TABLE campaigns
    DROP CONSTRAINT IF EXISTS campaigns_follow_up_sender_check;
ALTER TABLE campaigns
    ADD CONSTRAINT campaigns_follow_up_sender_check
    CHECK (follow_up_sender IN ('same', 'same_domain', 'rotate')) NOT VALID;
ALTER TABLE campaigns VALIDATE CONSTRAINT campaigns_follow_up_sender_check;
