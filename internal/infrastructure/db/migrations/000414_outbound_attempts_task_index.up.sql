-- Alone in its file for CONCURRENTLY. The send recovery checks probe outbound_attempts by task.
CREATE INDEX CONCURRENTLY IF NOT EXISTS outbound_attempts_task_id
    ON public.outbound_attempts (task_id) WHERE task_id IS NOT NULL;
