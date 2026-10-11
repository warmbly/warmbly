-- Per-step sender rotation.
--
-- When enabled on a campaign, each step of a contact's sequence may leave from
-- a different mailbox in the campaign's sender pool, chosen with the campaign's
-- rotation mode, instead of keeping the one that sent the previous step. The
-- immediately previous mailbox is excluded while another eligible one exists.
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS rotate_sender_per_step boolean NOT NULL DEFAULT false;
