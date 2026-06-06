-- Action / wait sequence nodes. A step can now be an EMAIL node (the existing
-- behaviour) or a control-plane ACTION/WAIT node (tag/unsubscribe/notify/wait/
-- end) that runs a side effect and routes onward WITHOUT sending mail. Existing
-- rows default to 'email' so routing and sending are unchanged.
ALTER TABLE sequences
    ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'email',
    ADD COLUMN IF NOT EXISTS action jsonb NOT NULL DEFAULT '{}';

UPDATE sequences SET kind = 'email' WHERE kind IS NULL OR kind = '';

ALTER TABLE sequences
    ADD CONSTRAINT sequences_kind_chk CHECK (kind IN ('email', 'action', 'wait'));
