-- Email error severity enum
CREATE TYPE email_error_severity AS ENUM('CRITICAL', 'WARNING', 'INFORMATIONAL');

-- Email error resolve method enum
CREATE TYPE email_error_resolve_method AS ENUM('OAUTH', 'RETRY', 'RELOAD', 'NONE');

-- Email account errors table for storing errors from worker
CREATE TABLE email_account_errors (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    email_account_id UUID NOT NULL,
    user_id UUID NOT NULL,

    -- Error details
    error_code VARCHAR(100) NOT NULL,
    severity email_error_severity NOT NULL,
    resolve_method email_error_resolve_method NOT NULL DEFAULT 'NONE',

    -- Display information
    title VARCHAR(255) NOT NULL,
    message TEXT NOT NULL,
    user_message TEXT,
    action_required TEXT,

    -- Reference to task that caused the error (optional)
    task_id UUID,

    -- Resolution tracking
    resolved_at TIMESTAMPTZ,
    resolved_by VARCHAR(100),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    FOREIGN KEY (email_account_id) REFERENCES email_accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Index for finding unresolved errors by account
CREATE INDEX idx_email_account_errors_unresolved
ON email_account_errors(email_account_id)
WHERE resolved_at IS NULL;

-- Index for finding all errors by user
CREATE INDEX idx_email_account_errors_user
ON email_account_errors(user_id, created_at DESC);

-- Index for finding errors by code (for batch resolution)
CREATE INDEX idx_email_account_errors_code
ON email_account_errors(email_account_id, error_code)
WHERE resolved_at IS NULL;
