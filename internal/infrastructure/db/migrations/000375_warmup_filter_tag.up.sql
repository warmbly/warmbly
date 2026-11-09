-- Opt-in code appended to warmup mail a mailbox receives, so its owner can filter it elsewhere. NULL is off.
ALTER TABLE public.email_accounts ADD COLUMN IF NOT EXISTS warmup_filter_tag text;
