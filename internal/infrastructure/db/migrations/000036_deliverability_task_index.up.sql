-- Supports the deliverability dashboard's per-mailbox breakdown, which joins
-- deliverability_events to tasks on task_id. Partial (task_id IS NOT NULL) keeps
-- it small since open/click/some events carry no task_id.
CREATE INDEX IF NOT EXISTS idx_deliverability_events_task
    ON deliverability_events (task_id)
    WHERE task_id IS NOT NULL;
