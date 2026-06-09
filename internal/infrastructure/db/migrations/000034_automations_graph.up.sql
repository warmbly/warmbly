-- Automations become a branching flow graph (trigger -> condition/action nodes
-- connected by edges) instead of a flat fan-out of step subscriptions. The graph
-- is the single source of truth; the executor walks it on each trigger event.

ALTER TABLE automations
    ADD COLUMN graph jsonb NOT NULL DEFAULT '{"nodes":[],"edges":[]}'::jsonb;

-- The previous builder persisted each action as an event-subscription tagged
-- use_case='automation'. Those are replaced by the graph; drop them so they
-- can't double-fire alongside the graph executor. (The automation_id column +
-- any hand-made standalone subscriptions are left intact.)
DELETE FROM integration_event_subscriptions WHERE use_case = 'automation';
