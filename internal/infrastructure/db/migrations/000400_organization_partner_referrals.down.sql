-- Refuse rather than discard the partner each referred workspace signed up through.
-- The lock holds off a referral written between the check and the drop.
LOCK TABLE public.organization_partner_referrals IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.organization_partner_referrals) THEN
        RAISE EXCEPTION 'organization_partner_referrals holds partner referrals; export or clear them before rolling back';
    END IF;
END $$;

DROP TABLE IF EXISTS public.organization_partner_referrals;
