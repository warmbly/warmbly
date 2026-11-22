-- Domains and mailboxes a workspace buys through the instance's GraphKit
-- partner account. Written only when GRAPHKIT_API_KEY is set, so a self-host
-- that never configures it has no rows. Excluded from workspace export: the
-- purchases are provisioned and billed through this instance's partner account.

-- The workspace's GraphKit account and the registrant its domains are registered under.
CREATE TABLE public.store_accounts (
    organization_id uuid PRIMARY KEY REFERENCES public.organizations (id) ON DELETE CASCADE,
    graphkit_account_id text NOT NULL DEFAULT '',
    registrant jsonb,
    -- Digest of the connection identities last set on the account, so a change is pushed once.
    identity_digest text NOT NULL DEFAULT '',
    -- When the workspace was told its plan ended while it holds purchased mailboxes; cleared when a plan returns.
    plan_notice_at timestamptz,
    -- The workspace's mailbox billing day (1-28), from its first mailbox, and the next billing date.
    -- Every live mailbox is charged together, a few days before that date.
    bill_day smallint CHECK (bill_day BETWEEN 1 AND 28),
    next_bill_anchor timestamptz,
    -- Automatic reload the store turned on and keeps sized to the mailboxes; the values it last set
    -- tell a hand edit apart, after which the store leaves it alone. Decided once per workspace.
    reload_decided boolean NOT NULL DEFAULT false,
    reload_managed boolean NOT NULL DEFAULT false,
    reload_threshold_micros bigint NOT NULL DEFAULT 0,
    reload_amount_micros bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- A quote with the retail prices shown to the buyer, so the order charges exactly them.
CREATE TABLE public.store_quotes (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    graphkit_quote_id text NOT NULL,
    snapshot jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_store_quotes_org ON public.store_quotes (organization_id, created_at DESC);
CREATE INDEX idx_store_quotes_expires ON public.store_quotes (expires_at);

CREATE TABLE public.store_orders (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    -- One order per quote: a retried checkout finds the order it already started.
    quote_id uuid NOT NULL UNIQUE,
    graphkit_order_id text UNIQUE,
    status text NOT NULL DEFAULT 'charging'
        CHECK (status IN ('charging', 'processing', 'completed', 'partially_failed', 'failed')),
    quote jsonb NOT NULL,
    steps jsonb NOT NULL DEFAULT '[]'::jsonb,
    due_now_micros bigint NOT NULL CHECK (due_now_micros >= 0),
    -- The ledger idempotency key for the checkout debit; its refund uses charge_key || ':refund'.
    charge_key text NOT NULL UNIQUE,
    charged_micros bigint NOT NULL DEFAULT 0 CHECK (charged_micros >= 0),
    refunded_micros bigint NOT NULL DEFAULT 0 CHECK (refunded_micros >= 0),
    -- Item keys already refunded, so a refunded item is credited once.
    refunded_items text[] NOT NULL DEFAULT '{}',
    forwarding_url text NOT NULL DEFAULT '',
    failure_code text NOT NULL DEFAULT '',
    created_by uuid REFERENCES public.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz
);
CREATE INDEX idx_store_orders_org ON public.store_orders (organization_id, created_at DESC, id DESC);
-- The reconciler reads only orders still in flight.
CREATE INDEX idx_store_orders_open ON public.store_orders (updated_at)
    WHERE status IN ('charging', 'processing');

CREATE TABLE public.store_domains (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    order_id uuid REFERENCES public.store_orders (id) ON DELETE SET NULL,
    graphkit_domain_id text NOT NULL UNIQUE,
    name text NOT NULL,
    status text NOT NULL,
    -- purchased: registered through the store; connected: registered elsewhere and added by the workspace.
    source text NOT NULL DEFAULT 'purchased' CHECK (source IN ('purchased', 'connected')),
    provider text NOT NULL DEFAULT '' CHECK (provider IN ('', 'google', 'microsoft')),
    expires_at timestamptz,
    auto_renew boolean NOT NULL DEFAULT true,
    -- A display cache of the current renewal price; a renewal reads the live price.
    renew_retail_micros bigint NOT NULL DEFAULT 0 CHECK (renew_retail_micros >= 0),
    -- The renewal price the workspace was last told, so a notice can say it went up.
    last_seen_renew_micros bigint NOT NULL DEFAULT 0 CHECK (last_seen_renew_micros >= 0),
    forwarding_url text NOT NULL DEFAULT '',
    mail_auth jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Display state read back as a whole: nameservers, connection, conflicts, owner verification, transfer.
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- The open and click tracking host the store set up on the domain; empty until it has.
    tracking_host text NOT NULL DEFAULT '',
    renewal_attempt_at timestamptz,
    renewal_failures integer NOT NULL DEFAULT 0,
    -- The last expiry notice sent, so each one goes out once per expiry.
    last_notice text NOT NULL DEFAULT '',
    -- When the upcoming-renewal notice for the current expiry went out; the charge waits a week after it.
    renewal_notice_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_store_domains_org ON public.store_domains (organization_id, created_at DESC, id DESC);
CREATE INDEX idx_store_domains_expiry ON public.store_domains (expires_at)
    WHERE status IN ('active', 'expired');

CREATE TABLE public.store_mailboxes (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    domain_id uuid NOT NULL REFERENCES public.store_domains (id) ON DELETE CASCADE,
    order_id uuid REFERENCES public.store_orders (id) ON DELETE SET NULL,
    graphkit_mailbox_id text NOT NULL UNIQUE,
    email text NOT NULL,
    provider text NOT NULL CHECK (provider IN ('google', 'microsoft')),
    status text NOT NULL,
    first_name text NOT NULL DEFAULT '',
    last_name text NOT NULL DEFAULT '',
    -- The domain's Workspace or tenant admin.
    is_admin boolean NOT NULL DEFAULT false,
    monthly_retail_micros bigint NOT NULL DEFAULT 0 CHECK (monthly_retail_micros >= 0),
    -- The end of the last GraphKit period Warmbly has collected for; the next period starts here.
    paid_through timestamptz NOT NULL,
    -- GraphKit's current_period_end; once known, paid_through is aligned to it (paid_aligned).
    period_end timestamptz,
    paid_aligned boolean NOT NULL DEFAULT false,
    -- Cancelled at GraphKit because its month went unpaid; paying before cancels_at resumes it.
    unpaid_cancel boolean NOT NULL DEFAULT false,
    cancels_at timestamptz,
    -- When the mailbox was found cancelled and its Warmbly mailbox switched off; set once.
    cancelled_at timestamptz,
    email_account_id uuid REFERENCES public.email_accounts (id) ON DELETE SET NULL,
    connect_method text NOT NULL DEFAULT '',
    connect_state text NOT NULL DEFAULT 'waiting'
        CHECK (connect_state IN ('waiting', 'connecting', 'connected', 'failed')),
    connect_error text NOT NULL DEFAULT '',
    connect_attempts integer NOT NULL DEFAULT 0,
    connect_attempted_at timestamptz,
    billing_failures integer NOT NULL DEFAULT 0,
    billing_attempted_at timestamptz,
    -- Set before a monthly debit and cleared after paid_through moves, so a
    -- retried batch replays the same ledger key instead of charging twice.
    pending_charge_key text,
    last_notice text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_store_mailboxes_org ON public.store_mailboxes (organization_id, created_at DESC, id DESC);
CREATE INDEX idx_store_mailboxes_due ON public.store_mailboxes (paid_through)
    WHERE status = 'active' AND cancels_at IS NULL;
CREATE INDEX idx_store_mailboxes_account ON public.store_mailboxes (email_account_id)
    WHERE email_account_id IS NOT NULL;
CREATE INDEX idx_store_mailboxes_ended ON public.store_mailboxes (updated_at)
    WHERE status IN ('cancelled', 'failed') AND cancelled_at IS NULL;

-- Webhook event ids already applied. Instance-local dedupe only.
CREATE TABLE public.store_webhook_events (
    event_id text PRIMARY KEY,
    received_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_store_webhook_events_received ON public.store_webhook_events (received_at);

-- A store purchase or renewal the balance does not cover is charged to the
-- saved card as its own kind, keyed by the store charge so a retry finds it,
-- with its receipt line for the invoice.
ALTER TABLE public.credit_auto_topup_attempts DROP CONSTRAINT IF EXISTS credit_auto_topup_attempts_kind_check;
ALTER TABLE public.credit_auto_topup_attempts
    ADD CONSTRAINT credit_auto_topup_attempts_kind_check CHECK (kind IN ('auto_reload', 'top_up', 'store')),
    ADD COLUMN charge_key text,
    ADD COLUMN description text NOT NULL DEFAULT '';
CREATE UNIQUE INDEX credit_auto_topup_attempts_charge_key
    ON public.credit_auto_topup_attempts (organization_id, charge_key) WHERE charge_key IS NOT NULL;

-- A grant GraphKit made in a purchased domain's Workspace for this instance's
-- service account. Its health is read through one of its mailboxes, since the
-- Workspace administrator is GraphKit's, not the customer's.
ALTER TABLE public.mailbox_domain_grants
    ADD COLUMN purchased boolean NOT NULL DEFAULT false;
