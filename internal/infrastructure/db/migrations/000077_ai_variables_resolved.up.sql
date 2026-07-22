-- Per-recipient AI variable resolution cache. Each campaign email body may
-- contain AI blocks that generate unique copy per contact at send time; the
-- resolved text is cached on the progress row so a task redelivery reuses the
-- same output (send consistency) instead of re-generating and re-charging.
-- Keyed by (campaign_id, contact_id, sequence_id) like the rest of this table;
-- the jsonb maps var id -> resolved text.
ALTER TABLE campaign_contact_progress
    ADD COLUMN IF NOT EXISTS ai_variables_resolved jsonb NOT NULL DEFAULT '{}';
