-- Reverses 000149. Every granted plan is forgotten, so a workspace that was
-- paid only because an operator said so becomes unpaid again on the next
-- entitlement check. Nothing else is lost: plan_id was never written by a
-- grant, so a Stripe-backed workspace is already on the plan it pays for.
ALTER TABLE public.subscriptions
    DROP CONSTRAINT IF EXISTS subscriptions_managed_complete_check;

ALTER TABLE public.subscriptions
    DROP COLUMN IF EXISTS managed_at,
    DROP COLUMN IF EXISTS managed_by,
    DROP COLUMN IF EXISTS managed_reason,
    DROP COLUMN IF EXISTS managed_until,
    DROP COLUMN IF EXISTS managed_plan_id;
