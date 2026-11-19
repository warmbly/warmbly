DROP INDEX IF EXISTS public.idx_store_mailboxes_photo_pending;
ALTER TABLE public.store_mailboxes
    DROP COLUMN IF EXISTS photo_attempt_at,
    DROP COLUMN IF EXISTS photo_attempts,
    DROP COLUMN IF EXISTS photo_applied_id,
    DROP COLUMN IF EXISTS photo_id;
DROP TABLE IF EXISTS public.store_photos;
