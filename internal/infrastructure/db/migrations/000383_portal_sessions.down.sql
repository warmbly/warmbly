-- A portal session's limits live in this column, so it ends rather than outlive them.
UPDATE public.sessions SET revoked_at = now() WHERE portal_agency_id IS NOT NULL AND revoked_at IS NULL;
ALTER TABLE public.sessions DROP COLUMN IF EXISTS portal_agency_id;
ALTER TABLE public.agency_brands DROP CONSTRAINT IF EXISTS agency_brands_portal_sign_in_check;
ALTER TABLE public.agency_brands DROP COLUMN IF EXISTS portal_sign_in;
