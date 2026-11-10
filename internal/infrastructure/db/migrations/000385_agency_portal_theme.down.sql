ALTER TABLE public.agency_brands DROP CONSTRAINT IF EXISTS agency_brands_theme_object_check;
ALTER TABLE public.agency_brands
    DROP COLUMN IF EXISTS theme,
    DROP COLUMN IF EXISTS sign_in_image_url,
    DROP COLUMN IF EXISTS icon_url;
