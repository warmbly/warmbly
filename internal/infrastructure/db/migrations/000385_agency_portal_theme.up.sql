-- An agency's portal theme: a tab icon, a sign-in page image, and its look and wording as one object.
-- Expand only; the previous release names none of these columns.

ALTER TABLE public.agency_brands
    ADD COLUMN IF NOT EXISTS icon_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS sign_in_image_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS theme jsonb NOT NULL DEFAULT '{}'::jsonb;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'agency_brands_theme_object_check') THEN
        ALTER TABLE public.agency_brands
            ADD CONSTRAINT agency_brands_theme_object_check CHECK (jsonb_typeof(theme) = 'object');
    END IF;
END $$;
