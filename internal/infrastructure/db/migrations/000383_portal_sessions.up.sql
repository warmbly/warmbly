-- Where an agency's clients sign in, and the agency a session minted for its portal is held to.
-- Expand only; the previous release reads neither column.

ALTER TABLE public.agency_brands
    ADD COLUMN IF NOT EXISTS portal_sign_in text NOT NULL DEFAULT 'portal';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'agency_brands_portal_sign_in_check') THEN
        ALTER TABLE public.agency_brands
            ADD CONSTRAINT agency_brands_portal_sign_in_check CHECK (portal_sign_in IN ('portal', 'warmbly'));
    END IF;
END $$;

-- No foreign key: a deleted agency leaves the id naming no clients, so the session still reaches nothing.
ALTER TABLE public.sessions
    ADD COLUMN IF NOT EXISTS portal_agency_id uuid;

COMMENT ON COLUMN public.sessions.portal_agency_id IS
    'The agency whose verified portal domain this session was signed in for. Set: the session reaches only that agency''s client workspaces and no account-level routes.';
