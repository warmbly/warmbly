-- Warmup batch generation.
--
-- The offline warmup-content generator originally ran every model call
-- synchronously (one chat-completion per thread). The OpenAI Batch API is ~50%
-- cheaper and processes asynchronously (up to a 24h window), which is a much
-- better fit for bulk thread-bank refills where latency does not matter.
--
-- This migration extends warmup_generation_jobs so one job row can represent
-- either a synchronous run (mode='sync', the existing behaviour) or an async
-- batch run (mode='batch'). For batch runs we track the OpenAI batch ID, the
-- uploaded input file ID, the output file ID (populated when the batch
-- completes), the last-observed OpenAI batch status, and the requested
-- completion window. A background poller reconciles in-flight batch jobs against
-- the OpenAI batch status and ingests results when they finish.

ALTER TABLE ONLY public.warmup_generation_jobs
    ADD COLUMN mode text NOT NULL DEFAULT 'sync',
    ADD COLUMN batch_id text NOT NULL DEFAULT '',
    ADD COLUMN batch_input_file_id text NOT NULL DEFAULT '',
    ADD COLUMN batch_output_file_id text NOT NULL DEFAULT '',
    ADD COLUMN batch_status text NOT NULL DEFAULT '',
    ADD COLUMN completion_window text NOT NULL DEFAULT '24h';

-- The poller scans for in-flight batch jobs by mode + status, so index that.
CREATE INDEX idx_warmup_generation_jobs_batch ON public.warmup_generation_jobs USING btree (mode, status, batch_status);
