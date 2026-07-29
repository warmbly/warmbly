-- Campaign AI step: an "ai" sequence action node runs an instruction over the
-- contact and can pick one label from the step's configured set. The chosen
-- label is stored per (campaign, contact, step) so "ai_label" branch conditions
-- route on it deterministically at schedule time — the scheduler's polling path
-- never calls a model. Labels are user-defined per step, so this is free text
-- ('' = not labeled / AI step not run).
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS ai_label text NOT NULL DEFAULT '';
