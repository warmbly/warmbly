-- Warmup pool types
CREATE TYPE warmup_pool_type AS ENUM('free', 'premium');

-- Warmup pool table
CREATE TABLE warmup_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_type warmup_pool_type NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    max_participants INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Pool participants
CREATE TABLE warmup_pool_participants (
    pool_id UUID NOT NULL REFERENCES warmup_pools(id) ON DELETE CASCADE,
    email_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    blocked_at TIMESTAMPTZ,
    blocked_reason TEXT,
    spam_score INT NOT NULL DEFAULT 0,

    PRIMARY KEY (pool_id, email_account_id),
    CONSTRAINT valid_spam_score CHECK (spam_score >= 0 AND spam_score <= 100)
);

-- Spam reports tracking
CREATE TABLE warmup_spam_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    reported_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    message_id TEXT NOT NULL,
    report_type VARCHAR(50) NOT NULL, -- 'spam', 'important', 'read'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (reporter_account_id, message_id)
);

-- Admin actions log
CREATE TABLE warmup_admin_actions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_user_id UUID NOT NULL REFERENCES users(id),
    email_account_id UUID NOT NULL REFERENCES email_accounts(id),
    action VARCHAR(50) NOT NULL, -- 'block', 'unblock'
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_warmup_participants_account ON warmup_pool_participants(email_account_id);
CREATE INDEX idx_warmup_participants_pool ON warmup_pool_participants(pool_id) WHERE blocked_at IS NULL;
CREATE INDEX idx_warmup_spam_reports_reported ON warmup_spam_reports(reported_account_id);
CREATE INDEX idx_warmup_spam_reports_reporter ON warmup_spam_reports(reporter_account_id);

-- Create default pools
INSERT INTO warmup_pools (pool_type, name, description) VALUES
    ('free', 'Free Warmup Pool', 'Default pool for all users'),
    ('premium', 'Premium Warmup Pool', 'Premium pool for subscribers');

-- Warmup verification tokens (anti-abuse)
CREATE TABLE warmup_tokens (
    token UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    sender_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    recipient_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    consumed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '7 days')
);
CREATE INDEX idx_warmup_tokens_task ON warmup_tokens(task_id);
CREATE INDEX idx_warmup_tokens_recipient ON warmup_tokens(recipient_account_id);
CREATE INDEX idx_warmup_tokens_unconsumed ON warmup_tokens(token) WHERE consumed_at IS NULL;

-- Track invalid warmup token attempts per account
CREATE TABLE warmup_invalid_token_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email_account_id UUID NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    attempted_token TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_warmup_invalid_attempts_account ON warmup_invalid_token_attempts(email_account_id, created_at);
