-- A campaign may opt out of the reply-earned graduation ceiling; existing campaigns keep it.
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS warmup_graduation boolean NOT NULL DEFAULT true;

COMMENT ON COLUMN public.campaigns.warmup_graduation IS
    'Hold mailboxes leaving warmup to the reply-earned cold ceiling in this campaign. Off leaves the mailbox cap, the campaign limit and the campaign ramp.';
