DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM campaign_tasks ct
        LEFT JOIN task_dead_letters d ON d.task_id = ct.task_id AND d.status = 'pending'
        WHERE ct.dispatch_intent = 'send'
           OR (ct.dispatch_intent = 'unverified' AND d.id IS NOT NULL)
    ) THEN
        RAISE EXCEPTION 'campaign dispatch intent is restrictive recovery evidence; retain it and roll forward';
    END IF;
END;
$$;

DROP TRIGGER campaign_dispatch_intent_monotonic ON campaign_tasks;
DROP FUNCTION retain_campaign_dispatch_intent();
ALTER TABLE campaign_tasks DROP COLUMN dispatch_intent;
DROP TYPE campaign_dispatch_intent;
