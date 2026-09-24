-- Alone in its file for CONCURRENTLY. The placement sweep walks uncounted
-- receipts oldest first; once they are counted the partial index is small.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warmup_received_unplaced
    ON warmup_received (created_at)
    WHERE NOT placed;
