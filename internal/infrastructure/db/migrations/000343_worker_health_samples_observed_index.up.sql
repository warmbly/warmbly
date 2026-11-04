-- Alone in its file for CONCURRENTLY. Serves the capacity view's last-hour window and the sample prune.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_worker_health_samples_observed
    ON public.worker_health_samples (observed_at);
