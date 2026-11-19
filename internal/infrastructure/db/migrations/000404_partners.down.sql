DROP TABLE IF EXISTS public.partner_events;
DROP TABLE IF EXISTS public.partner_idempotency_keys;
DROP TABLE IF EXISTS public.partner_identities;
DROP TABLE IF EXISTS public.partner_organizations;
DROP TABLE IF EXISTS public.partner_api_keys;
DROP TABLE IF EXISTS public.partners;
ALTER TABLE public.users DROP COLUMN IF EXISTS contact_email;
-- The plan stays while a subscription still names it.
DELETE FROM plans p WHERE p.id = '00000000-0000-0000-0000-0000000000c1'
    AND NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.plan_id = p.id OR s.managed_plan_id = p.id);
