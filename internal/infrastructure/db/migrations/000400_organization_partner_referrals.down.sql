-- Refuse rather than discard the partner each referred workspace signed up through.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.organization_partner_referrals) THEN
        RAISE EXCEPTION 'organization_partner_referrals holds partner referrals; export or clear them before rolling back';
    END IF;
END $$;

DROP TABLE IF EXISTS public.organization_partner_referrals;
