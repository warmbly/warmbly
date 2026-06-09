-- Automations: a named grouping of integration event-subscriptions that share
-- one trigger event. The dashboard's visual flow builder ("when meeting booked
-- fires -> notify Slack + create a deal") writes one subscription row per action
-- step, all tagged with the automation_id + the trigger event_type. Execution is
-- unchanged: the existing dispatcher already fans a fired event out to every
-- enabled matching subscription, so the steps just run.

CREATE TABLE automations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name text NOT NULL DEFAULT 'Automation',
    enabled boolean NOT NULL DEFAULT true,
    trigger_event text NOT NULL,
    filter jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now()
);

CREATE INDEX idx_automations_org ON automations (organization_id, created_at DESC);

-- Steps are ordinary event-subscriptions tagged with their automation. NULL =
-- a legacy/standalone subscription (still executes, just not shown as part of an
-- automation). CASCADE so deleting an automation removes its steps.
ALTER TABLE integration_event_subscriptions
    ADD COLUMN automation_id uuid REFERENCES automations (id) ON DELETE CASCADE;

CREATE INDEX idx_event_subscriptions_automation
    ON integration_event_subscriptions (automation_id);
