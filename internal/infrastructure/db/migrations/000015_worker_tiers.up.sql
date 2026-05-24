-- Worker type enum
CREATE TYPE worker_type AS ENUM ('shared', 'dedicated');

-- Extend workers table
ALTER TABLE workers
ADD COLUMN worker_type worker_type NOT NULL DEFAULT 'shared',
ADD COLUMN account_count INT NOT NULL DEFAULT 0,
ADD COLUMN free_tier BOOLEAN NOT NULL DEFAULT true;

-- Create index for load balancing queries
CREATE INDEX idx_workers_shared_load ON workers(account_count)
    WHERE worker_type = 'shared' AND active = true;

-- Index for free tier workers
CREATE INDEX idx_workers_free_tier ON workers(account_count)
    WHERE worker_type = 'shared' AND active = true AND free_tier = true;

-- Index for premium tier workers
CREATE INDEX idx_workers_premium_tier ON workers(account_count)
    WHERE worker_type = 'shared' AND active = true AND free_tier = false;

-- Extend plans table for dedicated worker support
ALTER TABLE plans
ADD COLUMN dedicated_workers INT NOT NULL DEFAULT 0,
ADD COLUMN daily_campaign_limit INT;

-- Dedicated worker assignments (for paid users with dedicated plans)
CREATE TABLE dedicated_worker_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    worker_id UUID NOT NULL REFERENCES workers(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subscription_id UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ,

    CONSTRAINT unique_active_worker UNIQUE (worker_id, released_at),
    CONSTRAINT unique_active_user_assignment UNIQUE (user_id, released_at)
);

CREATE INDEX idx_dedicated_user ON dedicated_worker_assignments(user_id) WHERE released_at IS NULL;
CREATE INDEX idx_dedicated_worker ON dedicated_worker_assignments(worker_id) WHERE released_at IS NULL;

-- Free trial fields on subscriptions
ALTER TABLE subscriptions
ADD COLUMN free_trial_started_at TIMESTAMPTZ,
ADD COLUMN free_trial_ends_at TIMESTAMPTZ;

CREATE INDEX idx_trial_expiry ON subscriptions(free_trial_ends_at)
    WHERE free_trial_ends_at IS NOT NULL;

-- Track email account's warmup pool type
ALTER TABLE email_accounts
ADD COLUMN warmup_pool_type VARCHAR(20) DEFAULT 'free';

