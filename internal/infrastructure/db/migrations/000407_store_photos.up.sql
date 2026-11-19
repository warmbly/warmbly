-- Profile photos uploaded for purchased mailboxes. The bytes are a private
-- normalised JPEG at blob_key; a row nothing used is swept after a day.
CREATE TABLE IF NOT EXISTS public.store_photos (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES public.organizations (id) ON DELETE CASCADE,
    blob_key text NOT NULL CHECK (blob_key LIKE 'store-photos/%'),
    size_bytes integer NOT NULL CHECK (size_bytes > 0),
    created_by uuid REFERENCES public.users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at timestamptz
);

CREATE INDEX IF NOT EXISTS idx_store_photos_org ON public.store_photos (organization_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_store_photos_unused ON public.store_photos (created_at) WHERE used_at IS NULL;

-- photo_id is the picture the mailbox should carry; photo_applied_id the one GraphKit last accepted.
ALTER TABLE public.store_mailboxes
    ADD COLUMN IF NOT EXISTS photo_id uuid REFERENCES public.store_photos (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS photo_applied_id uuid,
    ADD COLUMN IF NOT EXISTS photo_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS photo_attempt_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_store_mailboxes_photo_pending ON public.store_mailboxes (photo_attempt_at NULLS FIRST)
    WHERE photo_id IS NOT NULL AND photo_applied_id IS DISTINCT FROM photo_id;
