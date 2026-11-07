ALTER TABLE tasks ADD COLUMN campaign_send_isolated_at timestamptz,
    ADD CONSTRAINT tasks_isolated_campaign_send_check CHECK (
        campaign_send_isolated_at IS NULL OR task_type='campaign');

ALTER TABLE campaign_leads ADD COLUMN send_outcome_pending_at timestamptz;

CREATE INDEX campaign_leads_pending_send_outcome ON campaign_leads (campaign_id,contact_id)
    WHERE send_outcome_pending_at IS NOT NULL;

CREATE INDEX tasks_isolatable_unknown_campaign ON tasks (COALESCE(send_reserved_at,completed_at,updated_at),id)
    WHERE task_type='campaign' AND status IN ('completed','failed','dead_lettered')
      AND send_result_state='unknown' AND campaign_send_isolated_at IS NULL
      AND send_result_applied_at IS NULL AND send_released_at IS NULL;
CREATE INDEX tasks_unresolved_campaign_isolation ON tasks (id)
    WHERE campaign_send_isolated_at IS NOT NULL AND send_result_applied_at IS NULL;
CREATE INDEX idx_tasks_campaign_isolated_budget ON tasks (email_account_id,completed_at)
    WHERE task_type='campaign' AND campaign_send_isolated_at IS NOT NULL AND send_released_at IS NULL;

CREATE FUNCTION campaign_isolation_dispatch_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    original_task uuid;
BEGIN
    IF NEW.dispatched_at IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' THEN
        IF NEW.dispatched_at IS NOT DISTINCT FROM OLD.dispatched_at
           AND NEW.dispatch_task_id IS NOT DISTINCT FROM OLD.dispatch_task_id
           AND NEW.campaign_id=OLD.campaign_id AND NEW.contact_id=OLD.contact_id THEN
            RETURN NEW;
        END IF;
        original_task := OLD.dispatch_task_id;
    END IF;
    -- BEFORE INSERT must take the conflict row before the lead, just like ReserveSend.
    IF TG_OP='INSERT' THEN
        SELECT dispatch_task_id INTO original_task FROM campaign_contact_progress
        WHERE campaign_id=NEW.campaign_id AND contact_id=NEW.contact_id AND sequence_id=NEW.sequence_id
        FOR UPDATE;
    END IF;
    PERFORM 1 FROM campaign_leads
    WHERE campaign_id=NEW.campaign_id AND contact_id=NEW.contact_id FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM campaign_leads p
        WHERE p.campaign_id=NEW.campaign_id AND p.contact_id=NEW.contact_id AND p.send_outcome_pending_at IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM campaign_tasks ct JOIN tasks t ON t.id=ct.task_id
        WHERE ct.campaign_id=NEW.campaign_id AND ct.contact_id=NEW.contact_id
          AND t.campaign_send_isolated_at IS NOT NULL AND t.send_result_applied_at IS NULL
    ) THEN
        -- Only the reserved original task may receive a late receipt stamp.
        IF NEW.sent_at IS NOT NULL
           AND ((TG_OP='INSERT' AND NEW.dispatch_task_id IS NULL) OR NEW.dispatch_task_id=original_task)
           AND EXISTS (
               SELECT 1 FROM campaign_tasks ct JOIN tasks t ON t.id=ct.task_id
               WHERE t.id=original_task AND ct.campaign_id=NEW.campaign_id AND ct.contact_id=NEW.contact_id
                 AND ct.sequence_id=NEW.sequence_id AND t.campaign_send_isolated_at IS NOT NULL
                 AND t.send_result_applied_at IS NULL
           ) THEN
            RETURN NEW;
        END IF;
        RETURN NULL;
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER campaign_isolation_dispatch_guard
    BEFORE INSERT OR UPDATE ON campaign_contact_progress
    FOR EACH ROW EXECUTE FUNCTION campaign_isolation_dispatch_guard();

CREATE FUNCTION campaign_isolation_executor_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    campaign uuid;
    contact uuid;
BEGIN
    IF NEW.task_type<>'campaign' OR NEW.send_executor_started_at IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP='UPDATE' THEN
        IF OLD.send_executor_started_at IS NOT NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    IF NEW.campaign_send_isolated_at IS NOT NULL THEN
        RAISE EXCEPTION 'campaign send execution is isolated' USING ERRCODE='check_violation';
    END IF;
    SELECT ct.campaign_id,ct.contact_id INTO campaign,contact FROM campaign_tasks ct WHERE ct.task_id=NEW.id;
    PERFORM 1 FROM campaign_leads WHERE campaign_id=campaign AND contact_id=contact FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM campaign_leads p
        WHERE p.campaign_id=campaign AND p.contact_id=contact AND p.send_outcome_pending_at IS NOT NULL
    ) OR EXISTS (
        SELECT 1 FROM campaign_tasks ct JOIN tasks t ON t.id=ct.task_id
        WHERE ct.campaign_id=campaign AND ct.contact_id=contact
          AND t.campaign_send_isolated_at IS NOT NULL AND t.send_result_applied_at IS NULL
    ) THEN
        -- BeginOutbound must see an error because older callers do not inspect affected rows.
        RAISE EXCEPTION 'campaign lead has an unresolved isolated send' USING ERRCODE='check_violation';
    END IF;
    RETURN NEW;
END $$;

CREATE TRIGGER campaign_isolation_executor_guard
    BEFORE INSERT OR UPDATE OF send_executor_started_at ON tasks
    FOR EACH ROW EXECUTE FUNCTION campaign_isolation_executor_guard();
