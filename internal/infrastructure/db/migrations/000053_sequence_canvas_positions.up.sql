-- Persisted canvas coordinates for campaign sequence steps, so a teammate's
-- arrangement "sticks" across visits instead of being auto-laid-out on every
-- open. Written continuously as steps are dragged (a cosmetic, non-audited
-- layout patch). Default 0/0 means "no position yet": the editor auto-arranges
-- until a step is first moved.
ALTER TABLE sequences
    ADD COLUMN IF NOT EXISTS x double precision NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS y double precision NOT NULL DEFAULT 0;
