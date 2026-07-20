-- Compose drafts: autosaved, per-user working copies of unsent emails from the
-- compose window. Personal (user-scoped) rather than org-visible, so no audit
-- or realtime fanout; the id is client-generated for idempotent autosave.
CREATE TABLE IF NOT EXISTS public.compose_drafts (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    organization_id uuid NOT NULL REFERENCES public.organizations(id) ON DELETE CASCADE,
    email_account_id uuid REFERENCES public.email_accounts(id) ON DELETE SET NULL,
    to_addrs text[] NOT NULL DEFAULT '{}',
    cc text[] NOT NULL DEFAULT '{}',
    bcc text[] NOT NULL DEFAULT '{}',
    subject text NOT NULL DEFAULT '',
    body text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_compose_drafts_user
    ON public.compose_drafts (user_id, organization_id, updated_at DESC);
