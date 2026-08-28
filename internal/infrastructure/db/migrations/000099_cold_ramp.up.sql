-- Warmup-to-cold graduation (issue #147).
--
-- A mailbox at its warmup ceiling could join a campaign and send at the full
-- cold cap the same day: 40 warmup/day one morning, 50 cold/day the next. That
-- overnight jump is the post-warmup spike providers warn about.
--
-- cold_ramp_started_at is stamped on the mailbox's first cold send and anchors
-- a per-mailbox ceiling that climbs toward its cold cap over days. NULL means
-- the mailbox has not sent cold mail yet.
ALTER TABLE public.email_accounts
    ADD COLUMN cold_ramp_started_at timestamptz;

COMMENT ON COLUMN public.email_accounts.cold_ramp_started_at IS
    'First cold send; anchors the graduation ramp toward campaign_limit. NULL until the mailbox sends cold mail.';
