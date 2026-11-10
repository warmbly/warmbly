-- Agencies: one billing account and one shared monthly send pool across many
-- client workspaces. Expand only; the previous release ignores all of it.

-- A non-null pool marks an agency plan; 0 is an agency plan with no pool cap.
ALTER TABLE plans
    ADD COLUMN IF NOT EXISTS monthly_send_pool integer CHECK (monthly_send_pool IS NULL OR monthly_send_pool >= 0);

CREATE TABLE IF NOT EXISTS agencies (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                    text NOT NULL CHECK (btrim(name) <> ''),
    owner_user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    plan_id                 uuid REFERENCES plans (id) ON DELETE SET NULL,
    stripe_customer_id      text,
    stripe_subscription_id  text UNIQUE,
    status                  text NOT NULL DEFAULT 'incomplete' CHECK (status IN (
        'trialing', 'active', 'past_due', 'canceled', 'unpaid', 'incomplete', 'incomplete_expired', 'paused')),
    current_period_start    timestamptz,
    current_period_end      timestamptz,
    cancel_at_period_end    boolean NOT NULL DEFAULT false,
    -- The highest pool alert sent in the period that started at pool_alert_period.
    pool_alert_level        smallint NOT NULL DEFAULT 0 CHECK (pool_alert_level BETWEEN 0 AND 100),
    pool_alert_period       timestamptz,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agencies_owner ON agencies (owner_user_id);
CREATE INDEX IF NOT EXISTS idx_agencies_stripe_customer ON agencies (stripe_customer_id) WHERE stripe_customer_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS agency_members (
    agency_id  uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       text NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agency_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_agency_members_user ON agency_members (user_id);

-- Staff invited before they had an account; claimed by the verified address on sign-in.
CREATE TABLE IF NOT EXISTS agency_invitations (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agency_id  uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    email      text NOT NULL CHECK (btrim(email) <> ''),
    role       text NOT NULL CHECK (role IN ('admin', 'member')),
    invited_by uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS agency_invitations_agency_email_key ON agency_invitations (agency_id, lower(email));
CREATE INDEX IF NOT EXISTS idx_agency_invitations_email ON agency_invitations (lower(email));

-- One agency per workspace.
CREATE TABLE IF NOT EXISTS agency_clients (
    organization_id    uuid PRIMARY KEY REFERENCES organizations (id) ON DELETE CASCADE,
    agency_id          uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    monthly_send_limit integer CHECK (monthly_send_limit IS NULL OR monthly_send_limit >= 0),
    added_at           timestamptz NOT NULL DEFAULT now(),
    added_by           uuid REFERENCES users (id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_agency_clients_agency ON agency_clients (agency_id);

CREATE TABLE IF NOT EXISTS agency_pool_topups (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agency_id                uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    sends                    integer NOT NULL CHECK (sends > 0),
    amount_cents             bigint NOT NULL DEFAULT 0 CHECK (amount_cents >= 0),
    stripe_payment_intent_id text UNIQUE,
    period_start             timestamptz NOT NULL,
    -- A fully refunded pack leaves the pool.
    refunded_at              timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agency_pool_topups_period ON agency_pool_topups (agency_id, period_start);

-- Pool use per client and pool day, rolled up from campaign sends and only ever
-- raised, so detaching a client or deleting a campaign gives nothing back. No
-- foreign key on organization_id: a deleted client's sends still count.
CREATE TABLE IF NOT EXISTS agency_pool_usage_days (
    agency_id       uuid NOT NULL REFERENCES agencies (id) ON DELETE CASCADE,
    organization_id uuid NOT NULL,
    day             date NOT NULL,
    sends           integer NOT NULL CHECK (sends >= 0),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (agency_id, organization_id, day)
);

CREATE INDEX IF NOT EXISTS idx_agency_pool_usage_days_day ON agency_pool_usage_days (agency_id, day);

-- The agency whose subscription a managed grant mirrors; NULL is an operator grant.
ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS managed_by_agency_id uuid REFERENCES agencies (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_subscriptions_managed_by_agency ON subscriptions (managed_by_agency_id) WHERE managed_by_agency_id IS NOT NULL;

-- A membership the agency added to a client workspace, so removing staff or
-- detaching the workspace takes back only what the agency gave.
ALTER TABLE organization_members
    ADD COLUMN IF NOT EXISTS via_agency_id uuid REFERENCES agencies (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_organization_members_via_agency ON organization_members (via_agency_id) WHERE via_agency_id IS NOT NULL;

-- Agency plans. Never public to workspaces; the operator attaches the Stripe
-- prices on the admin plans page, and until then checkout reports them unavailable.
-- No monthly AI allowance: the balance is per workspace, so one per client would multiply with empty workspaces.
INSERT INTO plans (
    id, name, max_contacts, daily_emails, ai_generation, account_limit,
    price, price_yearly, discounted_price, duration_id, savings, public,
    dedicated_workers, daily_campaign_limit,
    max_campaigns, max_active_campaigns, max_team_members, max_email_accounts,
    monthly_allowance_micro, monthly_credits, monthly_send_pool
) VALUES
    ('00000000-0000-0000-0000-0000000000a1', 'Agency', 250000, 5000, true, 0,
     249, 2388, 249, '00000000-0000-0000-0000-0000000000d1', 20, false,
     0, 5000,
     NULL, NULL, NULL, NULL,
     0, 0, 150000),
    ('00000000-0000-0000-0000-0000000000a2', 'Agency Pro', 1000000, 16000, true, 0,
     599, 5748, 599, '00000000-0000-0000-0000-0000000000d1', 20, false,
     1, 16000,
     NULL, NULL, NULL, NULL,
     0, 0, 500000)
ON CONFLICT (id) DO NOTHING;
