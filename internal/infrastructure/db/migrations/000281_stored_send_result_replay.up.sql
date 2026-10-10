CREATE INDEX CONCURRENTLY tasks_pending_executor_results ON tasks(id)
    WHERE send_executor_result IS NOT NULL AND send_result_applied_at IS NULL;
