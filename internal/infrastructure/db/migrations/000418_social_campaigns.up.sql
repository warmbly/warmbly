-- Social steps in sequences, the per-contact social signals they branch on, and background LinkedIn
-- imports. Additive: older releases never read these tables.

-- The social accounts a campaign runs its social steps from, across channels.
CREATE TABLE public.campaign_social_accounts (
    campaign_id uuid NOT NULL REFERENCES public.campaigns (id) ON DELETE CASCADE,
    social_account_id uuid NOT NULL REFERENCES public.social_accounts (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (campaign_id, social_account_id)
);
CREATE INDEX idx_campaign_social_accounts_account ON public.campaign_social_accounts (social_account_id);

-- One social step queued at GraphKit for one contact. The (campaign, contact, step) row is the
-- reservation: it is written before GraphKit is asked, so a step is queued at most once.
CREATE TABLE public.social_actions (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    graphkit_id text UNIQUE,
    social_account_id uuid REFERENCES public.social_accounts (id) ON DELETE SET NULL,
    campaign_id uuid REFERENCES public.campaigns (id) ON DELETE SET NULL,
    contact_id uuid NOT NULL REFERENCES public.contacts (id) ON DELETE CASCADE,
    sequence_id uuid REFERENCES public.sequences (id) ON DELETE SET NULL,
    channel text NOT NULL CHECK (channel IN ('linkedin', 'instagram', 'whatsapp', 'telegram', 'facebook')),
    type text NOT NULL CHECK (type IN ('invite', 'message', 'inmail', 'voice_note', 'profile_visit', 'follow',
        'like_post', 'comment_post', 'endorse', 'withdraw_invite')),
    status text NOT NULL CHECK (status IN ('pending', 'queued', 'running', 'succeeded', 'skipped', 'failed', 'cancelled', 'expired')),
    skip_reason text,
    error_code text,
    reference text NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    queued_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_social_actions_step ON public.social_actions (campaign_id, contact_id, sequence_id)
    WHERE campaign_id IS NOT NULL AND sequence_id IS NOT NULL;
CREATE INDEX idx_social_actions_contact ON public.social_actions (organization_id, contact_id, created_at DESC);
CREATE INDEX idx_social_actions_campaign ON public.social_actions (campaign_id, type, status) WHERE campaign_id IS NOT NULL;

-- What a campaign's contact and the social account it is with on a channel are to each other, fed by
-- action results and GraphKit events. social_account_id is the account the contact stays with.
CREATE TABLE public.social_contact_state (
    campaign_id uuid NOT NULL REFERENCES public.campaigns (id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES public.contacts (id) ON DELETE CASCADE,
    channel text NOT NULL CHECK (channel IN ('linkedin', 'instagram', 'whatsapp', 'telegram', 'facebook')),
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    social_account_id uuid REFERENCES public.social_accounts (id) ON DELETE SET NULL,
    -- When the account first formed a relationship (invite, message, InMail, voice note) the contact keeps.
    bound_at timestamptz,
    invite_sent_at timestamptz,
    invite_pending boolean NOT NULL DEFAULT false,
    connected_at timestamptz,
    last_message_at timestamptz,
    replied_at timestamptz,
    -- LinkedIn Open Profile as the profiles call last read it; NULL is not looked up yet.
    open_profile boolean,
    open_profile_checked_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (campaign_id, contact_id, channel)
);
CREATE INDEX idx_social_contact_state_contact ON public.social_contact_state (organization_id, contact_id);
CREATE INDEX idx_social_contact_state_account ON public.social_contact_state (social_account_id) WHERE social_account_id IS NOT NULL;

-- A LinkedIn import that looks up work emails, run in the background.
CREATE TABLE public.linkedin_imports (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    user_id uuid REFERENCES public.users (id) ON DELETE SET NULL,
    status text NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    total integer NOT NULL DEFAULT 0,
    processed integer NOT NULL DEFAULT 0,
    created integer NOT NULL DEFAULT 0,
    updated integer NOT NULL DEFAULT 0,
    skipped integer NOT NULL DEFAULT 0,
    no_email integer NOT NULL DEFAULT 0,
    emails_found integer NOT NULL DEFAULT 0,
    contact_ids uuid[] NOT NULL DEFAULT '{}',
    request jsonb NOT NULL,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_linkedin_imports_org ON public.linkedin_imports (organization_id, created_at DESC);
CREATE INDEX idx_linkedin_imports_open ON public.linkedin_imports (updated_at) WHERE status IN ('queued', 'running');
