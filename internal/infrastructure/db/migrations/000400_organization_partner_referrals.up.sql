-- A workspace that signed up through a partner-program link (Dub): the click it
-- came from and when the signup reached Dub. Written only when DUB_API_KEY is
-- set, so a self-host never has a row. Instance-local, and excluded from
-- workspace export: the click and the customer it became live in this
-- instance's Dub workspace.
CREATE TABLE public.organization_partner_referrals (
    organization_id uuid PRIMARY KEY REFERENCES public.organizations(id) ON DELETE CASCADE,
    dub_click_id text NOT NULL CHECK (char_length(dub_click_id) BETWEEN 1 AND 64),
    lead_reported_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL DEFAULT now()
);
