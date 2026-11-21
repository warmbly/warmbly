-- The sign-in links the store minted for purchased Google mailboxes that connect by sign-in
-- (GraphKit connection method oauth): the mail vendor's team opens one from GraphKit and signs the
-- mailbox in. A link's id is the client connect link's; the row says which mailbox it is for, so
-- the connect lands on that mailbox and its login hint is the mailbox's address. Additive: older
-- releases never read it.
CREATE TABLE public.store_signin_links (
    link_id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    store_mailbox_id uuid NOT NULL REFERENCES public.store_mailboxes (id) ON DELETE CASCADE,
    email text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_store_signin_links_mailbox ON public.store_signin_links (store_mailbox_id);
