-- AI action nodes in automations (M9). A per-automation counter of consecutive
-- "out of AI credits" failures; when it reaches the pause threshold the flow is
-- auto-disabled so a broke org's automation stops burning against a hard wall
-- instead of failing an AI node on every event forever. Reset to 0 on any
-- successful AI node (or when the flow is re-enabled).
ALTER TABLE automations
    ADD COLUMN IF NOT EXISTS ai_credit_failures INT NOT NULL DEFAULT 0;
