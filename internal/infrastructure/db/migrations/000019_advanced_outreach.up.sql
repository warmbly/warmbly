-- Task statuses used by current and new task orchestration paths.
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'cancelled';
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'skipped_trial_expired';
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'skipped_daily_limit';
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'skipped_suppressed';
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'skipped_no_warmup_access';
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'dead_lettered';

ALTER TYPE campaign_status ADD VALUE IF NOT EXISTS 'completed';
ALTER TYPE campaign_status ADD VALUE IF NOT EXISTS 'paused_trial_expired';
ALTER TYPE campaign_status ADD VALUE IF NOT EXISTS 'paused_no_accounts';

CREATE TABLE IF NOT EXISTS outreach_settings (
    organization_id UUID PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by UUID REFERENCES users(id),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS campaign_advanced_settings (
    campaign_id UUID PRIMARY KEY REFERENCES campaigns(id) ON DELETE CASCADE,
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS campaign_ab_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    weight INT NOT NULL DEFAULT 100,
    subject TEXT NOT NULL DEFAULT '',
    body_html TEXT NOT NULL DEFAULT '',
    body_plain TEXT NOT NULL DEFAULT '',
    is_control BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT campaign_variant_weight_positive CHECK (weight > 0),
    CONSTRAINT campaign_variant_unique_name UNIQUE (campaign_id, name)
);

CREATE INDEX IF NOT EXISTS idx_campaign_ab_variants_campaign ON campaign_ab_variants(campaign_id);

CREATE TABLE IF NOT EXISTS campaign_ab_assignments (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES campaign_ab_variants(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    opened_at TIMESTAMPTZ,
    clicked_at TIMESTAMPTZ,
    replied_at TIMESTAMPTZ,
    bounced_at TIMESTAMPTZ,
    PRIMARY KEY (campaign_id, contact_id)
);

CREATE INDEX IF NOT EXISTS idx_campaign_ab_assignments_variant ON campaign_ab_assignments(variant_id);

CREATE TABLE IF NOT EXISTS deliverability_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    event_type VARCHAR(32) NOT NULL,
    provider VARCHAR(64) NOT NULL DEFAULT 'manual',
    recipient_email TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT deliverability_events_idempotency_unique UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_deliverability_events_org_created ON deliverability_events(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deliverability_events_campaign_created ON deliverability_events(campaign_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deliverability_events_type ON deliverability_events(event_type, created_at DESC);

CREATE TABLE IF NOT EXISTS suppressed_recipients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    source VARCHAR(32) NOT NULL,
    campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT suppressed_recipients_unique UNIQUE (organization_id, email)
);

CREATE INDEX IF NOT EXISTS idx_suppressed_recipients_org_email ON suppressed_recipients(organization_id, email);
CREATE INDEX IF NOT EXISTS idx_suppressed_recipients_org_updated ON suppressed_recipients(organization_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS task_execution_keys (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    execution_key TEXT NOT NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INT NOT NULL DEFAULT 1,
    status VARCHAR(32) NOT NULL DEFAULT 'in_progress',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (task_id, execution_key)
);

CREATE INDEX IF NOT EXISTS idx_task_execution_keys_status ON task_execution_keys(status, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS task_dead_letters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_type VARCHAR(32) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_error TEXT NOT NULL DEFAULT '',
    attempts INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL DEFAULT 5,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    next_retry_at TIMESTAMPTZ,
    replayed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_dead_letters_status ON task_dead_letters(status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_dead_letters_task ON task_dead_letters(task_id);

CREATE TABLE IF NOT EXISTS reply_intents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_email TEXT NOT NULL,
    campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    intent VARCHAR(32) NOT NULL,
    confidence NUMERIC(5,2) NOT NULL DEFAULT 0,
    action_taken TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reply_intents_org_created ON reply_intents(organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reply_intents_org_intent ON reply_intents(organization_id, intent, created_at DESC);

CREATE TABLE IF NOT EXISTS preflight_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    passed BOOLEAN NOT NULL,
    score INT NOT NULL,
    checks JSONB NOT NULL DEFAULT '[]'::jsonb,
    recommendations JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_preflight_reports_campaign_created ON preflight_reports(campaign_id, created_at DESC);
