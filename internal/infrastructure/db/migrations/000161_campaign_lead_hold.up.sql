-- A per-lead hold: one contact's flow inside ONE campaign is parked until a
-- moment (or until a person lifts it), without unsubscribing them and without
-- removing them from the campaign. Two things write it: an out-of-office
-- auto-reply, which parks the contact until they are back, and a member
-- pausing the lead by hand.
--
-- The "Subscribed" toggle and the suppression list were the only levers before
-- this, and both are workspace-wide and permanent (issue #470).
--
-- The hold is two timestamps and nothing else. How much of it counted against
-- the step's wait is DERIVED at routing time from the overlap between the hold
-- and the interval since the lead's last step, so there is no accumulator to
-- keep true and no path that has to remember to clear one.
ALTER TABLE campaign_leads
    -- paused_at IS NOT NULL is the hold itself. paused_until NULL means the
    -- hold has no end: only a person resumes it.
    ADD COLUMN paused_at    timestamptz,
    ADD COLUMN paused_until timestamptz,
    ADD COLUMN pause_reason text,
    ADD COLUMN pause_source text;

ALTER TABLE campaign_leads
    ADD CONSTRAINT campaign_leads_pause_source_check
    CHECK (pause_source IS NULL OR pause_source IN ('manual', 'out_of_office'));

-- A dispatch that finds the lead paused between routing and the send ends here
-- rather than sending, and rather than reading as a suppression in task
-- analytics. Postgres cannot drop an enum value, so the down migration leaves it.
ALTER TYPE public.task_status ADD VALUE IF NOT EXISTS 'skipped_paused';
