-- LinkedIn accounts a workspace connects through the instance's GraphKit partner account, billed
-- per seat from the workspace balance (managed) or by the workspace's own GraphKit plan (linked).
-- Additive: older releases never read these tables or the new contact columns.

-- A contact's LinkedIn profile, derived from its linkedin_url custom field so every write path
-- (the API, imports, integrations, workspace import) agrees. linkedin_public_id is the lowercase slug.
ALTER TABLE public.contacts
    ADD COLUMN IF NOT EXISTS linkedin_url text,
    ADD COLUMN IF NOT EXISTS linkedin_public_id text;

-- The slug of a linkedin.com/in/ URL; matches models.LinkedInPublicID for URLs.
CREATE FUNCTION public.linkedin_public_id(url text) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT NULLIF(lower(substring(url FROM '(?i)(?:^|[/.])linkedin\.com/in/([^/?#[:space:]"''<>\\]+)')), '')
$$;

CREATE FUNCTION public.contacts_linkedin() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.linkedin_public_id := public.linkedin_public_id(NEW.custom_fields->>'linkedin_url');
    NEW.linkedin_url := CASE WHEN NEW.linkedin_public_id IS NULL THEN NULL
        ELSE 'https://www.linkedin.com/in/' || NEW.linkedin_public_id END;
    RETURN NEW;
END $$;

CREATE TRIGGER contacts_linkedin
    BEFORE INSERT OR UPDATE OF custom_fields ON public.contacts
    FOR EACH ROW
    EXECUTE FUNCTION public.contacts_linkedin();

-- The workspace's LinkedIn mode as GraphKit last reported it.
CREATE TABLE public.linkedin_workspaces (
    organization_id uuid PRIMARY KEY REFERENCES public.organizations (id) ON DELETE CASCADE,
    mode text NOT NULL DEFAULT 'none' CHECK (mode IN ('none', 'managed', 'linked')),
    workspace_name text,
    linked_at timestamptz,
    -- The link session in flight, so a return to the dashboard completes the one this workspace started.
    link_session_id text,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE public.linkedin_accounts (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    -- Who connected it; attribution only.
    user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
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
CREATE INDEX idx_linkedin_accounts_org ON public.linkedin_accounts (organization_id, created_at DESC, id DESC);
CREATE INDEX idx_linkedin_accounts_due ON public.linkedin_accounts (paid_through)
    WHERE billing_mode = 'store' AND removed_at IS NULL;

-- A connect or reconnect at GraphKit and the first month it charged, refunded when it ends without an account.
CREATE TABLE public.linkedin_connect_sessions (
    id text PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
    kind text NOT NULL CHECK (kind IN ('connect', 'reconnect')),
    account_id uuid REFERENCES public.linkedin_accounts (id) ON DELETE SET NULL,
    charge_key text UNIQUE,
    charged_micros bigint NOT NULL DEFAULT 0 CHECK (charged_micros >= 0),
    status text NOT NULL,
    account_graphkit_id text,
    refunded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_linkedin_connect_sessions_org ON public.linkedin_connect_sessions (organization_id, created_at DESC);
-- The reconciler reads sessions still open and charges not yet settled.
CREATE INDEX idx_linkedin_connect_sessions_open ON public.linkedin_connect_sessions (updated_at)
    WHERE status IN ('sign_in', 'checkpoint') OR (charged_micros > 0 AND refunded_at IS NULL AND account_id IS NULL);

CREATE TABLE public.linkedin_conversations (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    linkedin_account_id uuid NOT NULL REFERENCES public.linkedin_accounts (id) ON DELETE CASCADE,
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
CREATE INDEX idx_linkedin_conversations_org ON public.linkedin_conversations (organization_id, last_message_at DESC NULLS LAST, id DESC);
CREATE INDEX idx_linkedin_conversations_account ON public.linkedin_conversations (linkedin_account_id);
CREATE INDEX idx_linkedin_conversations_contact ON public.linkedin_conversations (contact_id) WHERE contact_id IS NOT NULL;

CREATE TABLE public.linkedin_messages (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    conversation_id uuid NOT NULL REFERENCES public.linkedin_conversations (id) ON DELETE CASCADE,
    graphkit_id text NOT NULL UNIQUE,
    direction text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    -- The body sealed with the workspace's data key.
    body_sealed text NOT NULL DEFAULT '',
    sent_at timestamptz,
    status text NOT NULL DEFAULT 'sent',
    kind text NOT NULL DEFAULT 'message',
    linkedin_action_id uuid,
    sent_by_user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_linkedin_messages_conversation ON public.linkedin_messages (conversation_id, sent_at DESC NULLS FIRST, id DESC);

-- A named lead search; query is GraphKit's Query verbatim. Travels in workspace export.
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
