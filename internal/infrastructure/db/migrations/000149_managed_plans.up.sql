-- A plan an operator granted rather than Stripe. Needed for internal
-- workspaces, design partners and support gestures; before this the only way
-- to make a workspace paid was a Stripe subscription id, so the alternative
-- was hand-writing a fake one into this table and hoping no webhook ever
-- reconciled against an id Stripe has never heard of.
--
-- Mirrors organizations.risk_override_*: who, why, when, so the grant is
-- answerable months later. managed_until NULL is an open-ended grant; a value
-- expires the grant without anyone having to remember to revoke it.
ALTER TABLE public.subscriptions
    ADD COLUMN managed_at     timestamptz,
    ADD COLUMN managed_by     uuid REFERENCES public.users(id) ON DELETE SET NULL,
    ADD COLUMN managed_reason text,
    ADD COLUMN managed_until  timestamptz;

-- A grant is the four fields together. Recording one without a reason leaves
-- an unexplained paid workspace, which is the thing this is meant to prevent.
ALTER TABLE public.subscriptions
    ADD CONSTRAINT subscriptions_managed_complete_check
    CHECK (
        managed_at IS NULL
        OR (managed_reason IS NOT NULL AND btrim(managed_reason) <> '')
    );

-- The admin list filters on "managed by us", and an expiring grant is read on
-- every entitlement check, so both want an index rather than a scan.
CREATE INDEX idx_subscriptions_managed
    ON public.subscriptions (managed_at, managed_until)
    WHERE managed_at IS NOT NULL;
