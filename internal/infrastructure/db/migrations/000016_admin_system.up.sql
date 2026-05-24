-- User bans tracking
CREATE TABLE user_bans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    banned_by UUID NOT NULL REFERENCES users(id),
    reason TEXT NOT NULL,
    banned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    unbanned_at TIMESTAMPTZ,
    unbanned_by UUID REFERENCES users(id),
    unban_reason TEXT
);
CREATE INDEX idx_user_bans_user ON user_bans(user_id) WHERE unbanned_at IS NULL;
CREATE INDEX idx_user_bans_created ON user_bans(banned_at DESC);

-- Warmup appeals
CREATE TABLE warmup_appeals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    reason TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    review_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_warmup_appeals_status ON warmup_appeals(status) WHERE status = 'pending';
CREATE INDEX idx_warmup_appeals_account ON warmup_appeals(email_account_id);

-- Admin audit logs
CREATE TABLE admin_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_user_id UUID NOT NULL REFERENCES users(id),
    action VARCHAR(100) NOT NULL,
    target_type VARCHAR(50) NOT NULL,
    target_id UUID NOT NULL,
    details JSONB,
    ip_address TEXT,
    user_agent TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_admin_audit_admin ON admin_audit_logs(admin_user_id);
CREATE INDEX idx_admin_audit_target ON admin_audit_logs(target_type, target_id);
CREATE INDEX idx_admin_audit_action ON admin_audit_logs(action);
CREATE INDEX idx_admin_audit_created ON admin_audit_logs(created_at DESC);

-- Platform statistics snapshots
CREATE TABLE platform_statistics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    stat_date DATE NOT NULL UNIQUE,
    total_users INT NOT NULL DEFAULT 0,
    active_users INT NOT NULL DEFAULT 0,
    total_emails_sent INT NOT NULL DEFAULT 0,
    total_campaigns INT NOT NULL DEFAULT 0,
    active_campaigns INT NOT NULL DEFAULT 0,
    total_workers INT NOT NULL DEFAULT 0,
    active_workers INT NOT NULL DEFAULT 0,
    warmup_blocked_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Update enterprise_inquiries table for admin management
ALTER TABLE enterprise_inquiries
    ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS phone VARCHAR(50),
    ADD COLUMN IF NOT EXISTS monthly_email_volume VARCHAR(100),
    ADD COLUMN IF NOT EXISTS message TEXT,
    ADD COLUMN IF NOT EXISTS assigned_to UUID REFERENCES users(id),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT NOW();

CREATE INDEX IF NOT EXISTS idx_enterprise_inquiries_assigned ON enterprise_inquiries(assigned_to) WHERE assigned_to IS NOT NULL;
