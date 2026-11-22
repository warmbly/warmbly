-- Social accounts (LinkedIn, Instagram, WhatsApp, Telegram, Facebook) a workspace connects through the
-- instance's GraphKit partner account, billed per seat from the workspace balance (store) or by the
-- workspace's own GraphKit plan (linked). Additive: older releases never read these tables.

-- The workspace's social mode as GraphKit last reported it.
CREATE TABLE public.social_workspaces (
    organization_id uuid PRIMARY KEY REFERENCES public.organizations (id) ON DELETE CASCADE,
    mode text NOT NULL DEFAULT 'none' CHECK (mode IN ('none', 'managed', 'linked')),
    workspace_name text,
    linked_at timestamptz,
    -- The link session in flight, so a return to the dashboard completes the one this workspace started.
    link_session_id text,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE public.social_accounts (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    -- Who connected it; attribution only.
    user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
    channel text NOT NULL CHECK (channel IN ('linkedin', 'instagram', 'whatsapp', 'telegram', 'facebook')),
    graphkit_id text NOT NULL UNIQUE,
    status text NOT NULL,
    status_reason text,
    display_name text NOT NULL DEFAULT '',
    headline text NOT NULL DEFAULT '',
    avatar_url text NOT NULL DEFAULT '',
    profile_url text NOT NULL DEFAULT '',
    public_id text NOT NULL DEFAULT '',
    licences text[] NOT NULL DEFAULT '{}',
    -- GraphKit's SocialAccount as last read, shown as it is.
    snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
    billing_mode text NOT NULL CHECK (billing_mode IN ('store', 'linked')),
    monthly_retail_micros bigint NOT NULL DEFAULT 0 CHECK (monthly_retail_micros >= 0),
    -- The end of the last month Warmbly collected for this seat; NULL in linked mode.
    paid_through timestamptz,
    billing_failures integer NOT NULL DEFAULT 0,
    billing_attempted_at timestamptz,
    -- Set before a monthly debit and cleared after paid_through moves, so a retried run replays its key.
    pending_charge_key text,
    pending_charge_micros bigint NOT NULL DEFAULT 0 CHECK (pending_charge_micros >= 0),
    -- The ledger key and amount of the charge that collected the month ending at paid_through.
    paid_key text,
    paid_micros bigint NOT NULL DEFAULT 0 CHECK (paid_micros >= 0),
    -- When a month went unpaid; the seat is paused at GraphKit while unpaid_paused, removed a week later.
    unpaid_since timestamptz,
    unpaid_paused boolean NOT NULL DEFAULT false,
    last_notice text NOT NULL DEFAULT '',
    connected_at timestamptz,
    removed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_social_accounts_org ON public.social_accounts (organization_id, created_at DESC, id DESC);
CREATE INDEX idx_social_accounts_due ON public.social_accounts (paid_through)
    WHERE billing_mode = 'store' AND removed_at IS NULL;

-- A connect or reconnect at GraphKit and the first month it charged, refunded when it ends without an account.
CREATE TABLE public.social_connect_sessions (
    id text PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
    channel text NOT NULL CHECK (channel IN ('linkedin', 'instagram', 'whatsapp', 'telegram', 'facebook')),
    kind text NOT NULL CHECK (kind IN ('connect', 'reconnect')),
    -- The sign-in method last tried; NULL before the first attempt.
    method text CHECK (method IN ('password', 'cookie', 'qr', 'pairing_code')),
    account_id uuid REFERENCES public.social_accounts (id) ON DELETE SET NULL,
    charge_key text UNIQUE,
    charged_micros bigint NOT NULL DEFAULT 0 CHECK (charged_micros >= 0),
    status text NOT NULL,
    account_graphkit_id text,
    refunded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_social_connect_sessions_org ON public.social_connect_sessions (organization_id, created_at DESC);
-- The reconciler reads sessions still open and charges not yet settled.
CREATE INDEX idx_social_connect_sessions_open ON public.social_connect_sessions (updated_at)
    WHERE status IN ('sign_in', 'checkpoint') OR (charged_micros > 0 AND refunded_at IS NULL AND account_id IS NULL);

CREATE TABLE public.social_conversations (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    social_account_id uuid NOT NULL REFERENCES public.social_accounts (id) ON DELETE CASCADE,
    channel text NOT NULL CHECK (channel IN ('linkedin', 'instagram', 'whatsapp', 'telegram', 'facebook')),
    graphkit_id text NOT NULL UNIQUE,
    contact_id uuid REFERENCES public.contacts (id) ON DELETE SET NULL,
    participant jsonb NOT NULL DEFAULT '{}'::jsonb,
    kind text NOT NULL DEFAULT 'message',
    unread_count integer NOT NULL DEFAULT 0,
    last_message_at timestamptz,
    -- Plaintext like a unibox snippet; the bodies are sealed.
    last_message_preview text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_social_conversations_org ON public.social_conversations (organization_id, last_message_at DESC NULLS LAST, id DESC);
CREATE INDEX idx_social_conversations_account ON public.social_conversations (social_account_id);
CREATE INDEX idx_social_conversations_contact ON public.social_conversations (contact_id) WHERE contact_id IS NOT NULL;

CREATE TABLE public.social_messages (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES public.social_conversations (id) ON DELETE CASCADE,
    graphkit_id text NOT NULL UNIQUE,
    direction text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    -- The body sealed with the workspace's data key.
    body_sealed text NOT NULL DEFAULT '',
    sent_at timestamptz,
    status text NOT NULL DEFAULT 'sent',
    kind text NOT NULL DEFAULT 'message',
    social_action_id uuid,
    sent_by_user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
    -- When the reply path took an inbound message; NULL is not yet handled.
    reply_handled_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_social_messages_conversation ON public.social_messages (conversation_id, sent_at DESC NULLS FIRST, id DESC);

-- A named LinkedIn lead search; query is GraphKit's Query verbatim. Travels in workspace export.
CREATE TABLE public.linkedin_saved_searches (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    name text NOT NULL,
    query jsonb NOT NULL,
    created_by uuid REFERENCES public.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_linkedin_saved_searches_org ON public.linkedin_saved_searches (organization_id, created_at DESC, id DESC);

-- A contact's handle on each social channel and the consent basis outreach on that channel rests on.
-- normalized is models.NormalizeSocialHandle's form, the one lookups match on.
CREATE TABLE public.contact_social_identities (
    contact_id uuid NOT NULL REFERENCES public.contacts (id) ON DELETE CASCADE,
    channel text NOT NULL CHECK (channel IN ('linkedin', 'instagram', 'whatsapp', 'telegram', 'facebook')),
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    handle text NOT NULL,
    normalized text NOT NULL,
    consent_basis text CHECK (consent_basis IN ('opt_in', 'existing_conversation', 'imported_with_consent', 'friend')),
    consent_recorded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (contact_id, channel)
);
