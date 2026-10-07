ALTER TABLE warmup_received DROP COLUMN first_landing, DROP COLUMN first_folder, DROP COLUMN first_flags, DROP COLUMN observed_at, DROP COLUMN evidence;
ALTER TABLE warmup_placement_daily DROP COLUMN unknown, DROP COLUMN archived, DROP COLUMN custom, DROP COLUMN instrumented;
-- Keep the expanded verdict vocabulary: a rollback must not relabel observed copies.
ALTER TABLE placement_results DROP COLUMN first_landing, DROP COLUMN first_folder, DROP COLUMN first_flags, DROP COLUMN observed_at, DROP COLUMN evidence;
