-- A plan an operator granted rather than Stripe. Needed for internal
-- workspaces, design partners and support gestures; before this the only way
-- to make a workspace paid was a Stripe subscription id, so the alternative
-- was hand-writing a fake one into this table and hoping no webhook ever
-- reconciled against an id Stripe has never heard of.
--
-- Mirrors organizations.risk_override_*: who, why, when, so the grant is
-- answerable months later. managed_until NULL is an open-ended grant; a value
-- expires the grant without anyone having to remember to revoke it.
--
-- managed_plan_id is deliberately NOT plan_id. A workspace paying Stripe for
-- plan A that is granted plan B must go back to A when the grant ends, so the
-- grant is held beside the real plan rather than on top of it. Resolution is
-- models.Subscription.EffectivePlanID.
ALTER TABLE public.subscriptions
    ADD COLUMN managed_at      timestamptz,
    ADD COLUMN managed_by      uuid REFERENCES public.users(id) ON DELETE SET NULL,
    ADD COLUMN managed_reason  text,
    ADD COLUMN managed_until   timestamptz,
    ADD COLUMN managed_plan_id uuid REFERENCES public.plans(id) ON DELETE SET NULL;

-- A grant is the fields together. Recording one without a reason or without a
-- plan leaves an unexplained or inert paid workspace, which is the thing this
-- is meant to prevent.
ALTER TABLE public.subscriptions
    ADD CONSTRAINT subscriptions_managed_complete_check
    CHECK (
        managed_at IS NULL
        OR (
            managed_plan_id IS NOT NULL
            AND managed_reason IS NOT NULL
            AND btrim(managed_reason) <> ''
        )
    );
