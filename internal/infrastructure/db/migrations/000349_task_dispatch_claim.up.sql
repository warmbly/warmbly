-- A local dispatcher's claim on a due task; other replicas skip it until it lapses.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS dispatch_claimed_until timestamptz;
