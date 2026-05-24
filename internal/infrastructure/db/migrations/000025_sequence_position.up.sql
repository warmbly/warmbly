ALTER TABLE sequences ADD COLUMN IF NOT EXISTS position INT NOT NULL DEFAULT 0;

-- Backfill position based on existing creation order per campaign
WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY campaign_id ORDER BY created_at ASC) AS rn
    FROM sequences
)
UPDATE sequences SET position = ranked.rn
FROM ranked
WHERE sequences.id = ranked.id;
