CREATE TABLE users (
    id UUID NOT NULL DEFAULT gen_random_uuid (),
    first_name VARCHAR(255) NOT NULL,
    last_name VARCHAR(255) NOT NULL,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT,
    max_organizations INT NOT NULL DEFAULT 5,
    free_trial_used BOOLEAN NOT NULL DEFAULT FALSE,

    -- Admin fields
    admin_permissions INT NOT NULL DEFAULT 0,
    admin_granted_at TIMESTAMPTZ,
    admin_granted_by UUID,
    banned_at TIMESTAMPTZ,

    referral_source VARCHAR(50),
    onboarding_completed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ DEFAULT now (),
    updated_at TIMESTAMPTZ DEFAULT now (),

    PRIMARY KEY (id),
    UNIQUE (email),
    FOREIGN KEY (admin_granted_by) REFERENCES users(id)
);

CREATE INDEX idx_users_admin ON users(admin_permissions) WHERE admin_permissions > 0;
CREATE INDEX idx_users_banned ON users(banned_at) WHERE banned_at IS NOT NULL;

-- Organizations table
CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) UNIQUE,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_organizations_owner ON organizations(owner_user_id);
CREATE INDEX idx_organizations_slug ON organizations(slug);

-- Organization membership
CREATE TABLE organization_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    permissions SMALLINT NOT NULL DEFAULT 0,
    invited_by UUID REFERENCES users(id),
    invited_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,

    CONSTRAINT unique_org_member UNIQUE (organization_id, user_id)
);

CREATE INDEX idx_org_members_user ON organization_members(user_id);
CREATE INDEX idx_org_members_org ON organization_members(organization_id);

-- Organization invitations
CREATE TABLE organization_invitations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'viewer',
    permissions SMALLINT NOT NULL DEFAULT 0,
    invited_by UUID NOT NULL REFERENCES users(id),
    token VARCHAR(64) NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT unique_pending_org_invite UNIQUE (organization_id, email)
);

CREATE INDEX idx_org_invitations_token ON organization_invitations(token);
CREATE INDEX idx_org_invitations_email ON organization_invitations(email);

-- Enterprise inquiries
CREATE TABLE enterprise_inquiries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_name VARCHAR(255) NOT NULL,
    contact_name VARCHAR(255) NOT NULL,
    contact_email VARCHAR(255) NOT NULL,
    estimated_volume INT,
    team_size INT,
    notes TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    processed_by UUID REFERENCES users(id)
);

CREATE INDEX idx_enterprise_inquiries_status ON enterprise_inquiries(status);
CREATE INDEX idx_enterprise_inquiries_email ON enterprise_inquiries(contact_email);

CREATE TABLE sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid (),
    user_id UUID NOT NULL,
    current_organization_id UUID REFERENCES organizations(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,

    last_refreshed_at TIMESTAMPTZ,
    refresh_nonce TEXT,
    access_nonce TEXT,

    location_city TEXT,
    location_region TEXT,
    location_country TEXT,
    location_country_code TEXT,
    location_postal_code TEXT,

    os_name TEXT,
    browser_name TEXT,

    PRIMARY KEY (id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_sessions_current_org ON sessions(current_organization_id);

CREATE TABLE languages (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,

    PRIMARY KEY (id)
)
