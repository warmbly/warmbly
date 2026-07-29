-- AI skills: org-authored playbooks the AI features can load and follow. name +
-- description are listed in the agent/research/reply prompts; content is the
-- full markdown a load_skill tool returns on demand. Enabled skills are the
-- only ones surfaced to the models.
CREATE TABLE IF NOT EXISTS ai_skills (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    content     text NOT NULL DEFAULT '',
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_skills_org ON ai_skills (org_id, created_at DESC);

-- One skill name per org (the load_skill tool resolves by name).
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_skills_org_name ON ai_skills (org_id, lower(name));
