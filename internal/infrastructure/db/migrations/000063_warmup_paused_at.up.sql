-- Pause-aware warmup. warmup_paused_at lets a mailbox pause warmup without
-- losing ramp progress: on resume the warmup anchor (warmup) is shifted
-- forward by the paused duration so daily volume continues where it left
-- off instead of restarting from the base volume.
--
--   warmup IS NOT NULL AND warmup_paused_at IS NULL     => actively warming
--   warmup IS NOT NULL AND warmup_paused_at IS NOT NULL => paused (progress kept)
--   warmup IS NULL                                      => off
ALTER TABLE email_accounts
    ADD COLUMN IF NOT EXISTS warmup_paused_at TIMESTAMPTZ;
