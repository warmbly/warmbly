-- Track email status for each contact in each campaign sequence
CREATE TABLE campaign_contact_progress (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    sequence_id UUID NOT NULL REFERENCES sequences(id) ON DELETE CASCADE,

    sent_at TIMESTAMPTZ,
    opened_at TIMESTAMPTZ,
    clicked_at TIMESTAMPTZ,
    replied_at TIMESTAMPTZ,
    bounced_at TIMESTAMPTZ,

    PRIMARY KEY (campaign_id, contact_id, sequence_id)
);

-- Indexes for efficient queries
CREATE INDEX idx_campaign_progress_campaign ON campaign_contact_progress(campaign_id);
CREATE INDEX idx_campaign_progress_contact ON campaign_contact_progress(contact_id);
CREATE INDEX idx_campaign_progress_sent ON campaign_contact_progress(campaign_id, sent_at) WHERE sent_at IS NOT NULL;

-- CRM: Contact notes
CREATE TABLE contact_notes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_contact_notes_contact ON contact_notes(contact_id, created_at DESC);

-- CRM: Contact activities (timeline)
CREATE TYPE activity_type AS ENUM(
    'email_sent', 'email_opened', 'email_clicked', 'email_replied', 'email_bounced',
    'note_added', 'note_updated',
    'deal_created', 'deal_stage_changed', 'deal_won', 'deal_lost',
    'task_created', 'task_completed',
    'contact_created', 'contact_updated',
    'campaign_added', 'campaign_removed'
);

CREATE TABLE contact_activities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id),
    activity_type activity_type NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_contact_activities_contact ON contact_activities(contact_id, created_at DESC);
CREATE INDEX idx_contact_activities_org ON contact_activities(organization_id, created_at DESC);

-- CRM: Pipelines
CREATE TABLE pipelines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pipelines_org ON pipelines(organization_id);

-- CRM: Pipeline stages
CREATE TABLE pipeline_stages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    color VARCHAR(7) NOT NULL DEFAULT '#6366f1' CHECK (color ~* '^#[a-f0-9]{6}$'),
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_pipeline_stages_pipeline ON pipeline_stages(pipeline_id, position);

-- CRM: Deals
CREATE TYPE deal_status AS ENUM('open', 'won', 'lost');

CREATE TABLE deals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    pipeline_id UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    stage_id UUID NOT NULL REFERENCES pipeline_stages(id),
    contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL,
    value DECIMAL(12, 2),
    currency VARCHAR(3) DEFAULT 'USD',
    status deal_status NOT NULL DEFAULT 'open',
    expected_close_date DATE,
    won_at TIMESTAMPTZ,
    lost_at TIMESTAMPTZ,
    lost_reason TEXT,
    assigned_to UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_deals_org ON deals(organization_id);
CREATE INDEX idx_deals_pipeline ON deals(pipeline_id, stage_id);
CREATE INDEX idx_deals_contact ON deals(contact_id);

-- CRM: Tasks/Reminders
CREATE TYPE crm_task_priority AS ENUM('low', 'medium', 'high', 'urgent');
CREATE TYPE crm_task_status AS ENUM('pending', 'in_progress', 'completed', 'cancelled');

CREATE TABLE crm_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    assigned_to UUID REFERENCES users(id),
    created_by UUID NOT NULL REFERENCES users(id),
    title VARCHAR(255) NOT NULL,
    description TEXT,
    due_date TIMESTAMPTZ,
    priority crm_task_priority NOT NULL DEFAULT 'medium',
    status crm_task_status NOT NULL DEFAULT 'pending',
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_crm_tasks_org ON crm_tasks(organization_id);
CREATE INDEX idx_crm_tasks_contact ON crm_tasks(contact_id);
CREATE INDEX idx_crm_tasks_deal ON crm_tasks(deal_id);
CREATE INDEX idx_crm_tasks_assigned ON crm_tasks(assigned_to, status);

-- Campaign activity logs
CREATE TABLE campaign_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_campaign_logs_campaign ON campaign_logs(campaign_id, created_at DESC);
CREATE INDEX idx_campaign_logs_type ON campaign_logs(campaign_id, event_type, created_at DESC);
