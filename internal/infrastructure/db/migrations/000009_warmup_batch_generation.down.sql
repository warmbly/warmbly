DROP INDEX IF EXISTS idx_warmup_generation_jobs_batch;

ALTER TABLE ONLY public.warmup_generation_jobs
    DROP COLUMN IF EXISTS mode,
    DROP COLUMN IF EXISTS batch_id,
    DROP COLUMN IF EXISTS batch_input_file_id,
    DROP COLUMN IF EXISTS batch_output_file_id,
    DROP COLUMN IF EXISTS batch_status,
    DROP COLUMN IF EXISTS completion_window;
