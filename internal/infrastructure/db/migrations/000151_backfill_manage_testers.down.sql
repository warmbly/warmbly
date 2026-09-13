-- No-op on purpose. Clearing bit 22 would also revoke it from an admin who was
-- granted it explicitly after the backfill, and re-running the up migration
-- would not give it back to them.
SELECT 1;
