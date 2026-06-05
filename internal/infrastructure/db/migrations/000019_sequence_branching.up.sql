-- Per-step conditional branching. A sequence step can carry a branching tree
-- that routes a contact to a target step (or stops them) based on whether they
-- opened/clicked/replied within N days. Kept as a single jsonb tree on the
-- step itself — no extra tables. An empty object ('{}') / empty branches list
-- means the step keeps the default linear progression.
ALTER TABLE sequences ADD COLUMN IF NOT EXISTS conditions jsonb NOT NULL DEFAULT '{}';
