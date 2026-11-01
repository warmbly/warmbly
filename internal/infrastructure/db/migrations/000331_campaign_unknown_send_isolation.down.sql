DO $$
BEGIN
    LOCK TABLE tasks IN ACCESS EXCLUSIVE MODE;
    LOCK TABLE campaign_leads IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM tasks WHERE campaign_send_isolated_at IS NOT NULL)
       OR EXISTS (SELECT 1 FROM campaign_leads WHERE send_outcome_pending_at IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove permanent campaign send isolation evidence; retain the schema and roll forward';
    END IF;
END $$;

DROP TRIGGER campaign_isolation_executor_guard ON tasks;
DROP FUNCTION campaign_isolation_executor_guard();
DROP TRIGGER campaign_isolation_dispatch_guard ON campaign_contact_progress;
DROP FUNCTION campaign_isolation_dispatch_guard();
DROP INDEX tasks_unresolved_campaign_isolation;
DROP INDEX idx_tasks_campaign_isolated_budget;
DROP INDEX tasks_isolatable_unknown_campaign;
DROP INDEX campaign_leads_pending_send_outcome;
ALTER TABLE campaign_leads DROP COLUMN send_outcome_pending_at;
ALTER TABLE tasks DROP CONSTRAINT tasks_isolated_campaign_send_check,
    DROP COLUMN campaign_send_isolated_at;
