CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_crm_sync_jobs_running_lease
    ON public.crm_sync_jobs (provider, locked_until) WHERE status = 'running';
