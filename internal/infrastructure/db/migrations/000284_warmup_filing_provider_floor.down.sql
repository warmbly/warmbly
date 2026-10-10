UPDATE warmup_pending_filings
SET next_attempt_at = GREATEST(next_attempt_at, provider_retry_at)
WHERE provider_retry_at IS NOT NULL;

ALTER TABLE warmup_pending_filings DROP COLUMN provider_retry_at;
