CREATE TABLE tags (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    title VARCHAR(255) NOT NULL,
    color VARCHAR(7) NOT NULL ,
    position INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT valid_color CHECK (color ~* '^#[a-f0-9]{6}$')
);

CREATE TYPE email_provider AS ENUM('gmail', 'outlook', 'smtp_imap');
CREATE TYPE email_status AS ENUM('active', 'inactive', 'revoked');

CREATE TABLE email_accounts (
    id UUID NOT NULL,
    user_id UUID NOT NULL,
    organization_id UUID REFERENCES organizations(id),
    worker_id UUID,
    email VARCHAR(255) NOT NULL,

    name VARCHAR(255) NOT NULL,
    signature_plain TEXT NOT NULL,
    signature_html TEXT NOT NULL,
    signature_sync BOOLEAN NOT NULL DEFAULT TRUE,
    signature_code BOOLEAN NOT NULL DEFAULT FALSE,

    provider email_provider NOT NULL,
    status email_status NOT NULL DEFAULT 'active',

    last_synced_at TIMESTAMP,
    last_id BIGINT,

    campaign_limit INT NOT NULL DEFAULT 50,
    min_wait_time INT NOT NULL DEFAULT 600,
    reply_to TEXT NOT NULL DEFAULT '',

    tracking_domain TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'UTC',

    warmup TIMESTAMP,
    warmup_base INT NOT NULL DEFAULT 10,
    warmup_max INT NOT NULL DEFAULT 40,
    warmup_increase INT NOT NULL DEFAULT 1,
    warmup_reply_rate SMALLINT NOT NULL DEFAULT 30,
    warmup_tag TEXT NOT NULL,
    warmup_start_time TIME NOT NULL DEFAULT '08:00',
    warmup_end_time TIME NOT NULL DEFAULT '20:00',
    warmup_days SMALLINT NOT NULL DEFAULT 0,

    created_at TIMESTAMP NOT NULL DEFAULT NOW (),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW (),

    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT valid_reply_rate CHECK (warmup_reply_rate >= 0 AND warmup_reply_rate <= 100)
);

CREATE INDEX idx_email_accounts_org ON email_accounts(organization_id);

CREATE TABLE email_tags (
    email_id UUID NOT NULL,
    tag_id UUID NOT NULL,
    PRIMARY KEY (email_id, tag_id),
    FOREIGN KEY (email_id) REFERENCES email_accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE TABLE email_accounts_oauth (
    email_account_id UUID NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW (),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW (),

    PRIMARY KEY (email_account_id),
    FOREIGN KEY (email_account_id) REFERENCES email_accounts (id) ON DELETE CASCADE
);

CREATE TABLE email_accounts_smtp_imap (
    email_account_id UUID PRIMARY KEY NOT NULL REFERENCES email_accounts (id) ON DELETE CASCADE,
    smtp_host VARCHAR(255) NOT NULL,
    smtp_port INT NOT NULL,
    smtp_user VARCHAR(255) NOT NULL,
    smtp_password VARCHAR(255) NOT NULL,
    imap_host VARCHAR(255) NOT NULL,
    imap_port INT NOT NULL,
    imap_user VARCHAR(255) NOT NULL,
    imap_password VARCHAR(255) NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW ()
);

CREATE TABLE categories (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    title VARCHAR(255) NOT NULL,
    color VARCHAR(7) NOT NULL ,
    position INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT valid_color CHECK (color ~* '^#[a-f0-9]{6}$')
);


CREATE TYPE campaign_status AS ENUM(
    'draft',
    'active',
    'paused',
    'completed',
    'paused_trial_expired',
    'paused_no_accounts'
);

CREATE TABLE campaigns (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    organization_id UUID REFERENCES organizations(id),

    name VARCHAR(50) NOT NULL,
    description TEXT NOT NULL,
    status campaign_status NOT NULL DEFAULT 'draft',
    stop_on_reply BOOLEAN NOT NULL DEFAULT FALSE,
    open_tracking BOOLEAN NOT NULL DEFAULT FALSE,
    link_tracking BOOLEAN NOT NULL DEFAULT FALSE,
    text_only BOOLEAN NOT NULL DEFAULT FALSE,
    daily_limit INT NOT NULL DEFAULT 50,
    unsubscribe_header BOOLEAN NOT NULL DEFAULT TRUE,
    risky_emails BOOLEAN NOT NULL DEFAULT TRUE,

    cc_addr TEXT[] NOT NULL DEFAULT '{}',
    bcc_addr TEXT[] NOT NULL DEFAULT '{}',

    start_date TIMESTAMP,
    end_date TIMESTAMP,
    timezone TEXT NOT NULL DEFAULT 'Europe/London',
    days SMALLINT NOT NULL,
    start_time TIME NOT NULL DEFAULT '08:00',
    end_time TIME NOT NULL DEFAULT '18:00',

    last_status_change_at TIMESTAMPTZ,

    updated_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL,

    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE INDEX idx_campaigns_org ON campaigns(organization_id);

CREATE TABLE campaign_email_tags (
    tag_id UUID NOT NULL ,
    campaign_id UUID NOT NULL,
    PRIMARY KEY (campaign_id, tag_id),
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE,
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE CASCADE
);

CREATE TABLE sequences (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    campaign_id UUID NOT NULL,
    organization_id UUID REFERENCES organizations(id),

    name VARCHAR(50) NOT NULL,
    subject TEXT NOT NULL,

    body_plain TEXT NOT NULL,
    body_html TEXT NOT NULL,
    body_sync BOOLEAN NOT NULL DEFAULT TRUE,
    body_code BOOLEAN NOT NULL DEFAULT FALSE,

    wait_after INT NOT NULL DEFAULT 10,

    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE CASCADE
);

CREATE INDEX idx_sequences_org ON sequences(organization_id);

CREATE TABLE contacts (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    organization_id UUID REFERENCES organizations(id),

    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    email TEXT NOT NULL,
    company TEXT NOT NULL,
    phone TEXT NOT NULL,

    custom_fields JSONB NOT NULL,

    subscribed BOOLEAN DEFAULT TRUE,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_contacts_org ON contacts(organization_id);

CREATE TABLE campaign_leads (
    contact_id UUID NOT NULL,
    campaign_id UUID NOT NULL REFERENCES campaigns (id) ON DELETE CASCADE,
    PRIMARY KEY (campaign_id, contact_id),
    FOREIGN KEY (contact_id) REFERENCES contacts (id) ON DELETE CASCADE,
    FOREIGN KEY (campaign_id) REFERENCES campaigns (id) ON DELETE CASCADE
);

CREATE TABLE folders (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    title VARCHAR(255) NOT NULL,
    color VARCHAR(7) NOT NULL ,
    position INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT valid_color CHECK (color ~* '^#[a-f0-9]{6}$')
);

CREATE TABLE campaign_folders (
    campaign_id UUID NOT NULL,
    folder_id UUID NOT NULL,
    PRIMARY KEY (campaign_id, folder_id),
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE CASCADE,
    FOREIGN KEY (folder_id) REFERENCES folders(id) ON DELETE CASCADE
);

-- Reply templates (org-scoped)
CREATE TABLE reply_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    body_html TEXT NOT NULL DEFAULT '',
    body_plain TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_reply_templates_org ON reply_templates(organization_id);

-- Contact ordering settings for campaigns
ALTER TABLE campaigns ADD COLUMN contact_order_by VARCHAR(20) DEFAULT 'created_at';
ALTER TABLE campaigns ADD COLUMN contact_order_dir VARCHAR(4) DEFAULT 'asc';
ALTER TABLE campaigns ADD COLUMN contact_order_field TEXT;

-- Position column for manual contact ordering
ALTER TABLE campaign_leads ADD COLUMN position INTEGER;
