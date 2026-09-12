-- Reverses 000147. Labels go back to being owned by whoever created them.
--
-- Three things do not come back: the per-user position numbering the up
-- migration collapsed into one sequence per workspace, the duplicate
-- conversation labels it merged, and the identity of a label it had to copy
-- because two workspaces shared one row (the copy stays, as its own label).
-- All three are cosmetic; the labels themselves and everything they are
-- attached to survive the round trip.

BEGIN;

-- A label whose creator was deleted while the workspace owned it has no user
-- to go back to, so it adopts the organization's owner.
UPDATE public.unibox_thread_labels utl
SET user_id = o.owner_user_id
FROM public.organizations o
WHERE o.id = utl.organization_id AND utl.user_id IS NULL;
DELETE FROM public.unibox_thread_labels WHERE user_id IS NULL;

ALTER TABLE public.unibox_thread_labels DROP CONSTRAINT IF EXISTS unibox_thread_labels_organization_id_fkey;
ALTER TABLE public.unibox_thread_labels DROP CONSTRAINT IF EXISTS unibox_thread_labels_pkey;
ALTER TABLE public.unibox_thread_labels DROP COLUMN IF EXISTS organization_id;
ALTER TABLE public.unibox_thread_labels ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE public.unibox_thread_labels
    ADD CONSTRAINT unibox_thread_labels_pkey PRIMARY KEY (user_id, thread_id, category_id);

ALTER TABLE public.unibox_thread_labels DROP CONSTRAINT IF EXISTS unibox_thread_labels_user_id_fkey;
ALTER TABLE public.unibox_thread_labels
    ADD CONSTRAINT unibox_thread_labels_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_unibox_thread_labels_org_thread;
CREATE INDEX IF NOT EXISTS idx_unibox_thread_labels_thread
    ON public.unibox_thread_labels USING btree (user_id, thread_id);

UPDATE public.tags g       SET user_id = o.owner_user_id FROM public.organizations o WHERE o.id = g.organization_id AND g.user_id IS NULL;
UPDATE public.categories g SET user_id = o.owner_user_id FROM public.organizations o WHERE o.id = g.organization_id AND g.user_id IS NULL;
UPDATE public.folders g    SET user_id = o.owner_user_id FROM public.organizations o WHERE o.id = g.organization_id AND g.user_id IS NULL;

DELETE FROM public.tags       WHERE user_id IS NULL;
DELETE FROM public.categories WHERE user_id IS NULL;
DELETE FROM public.folders    WHERE user_id IS NULL;

DROP INDEX IF EXISTS idx_tags_organization;
DROP INDEX IF EXISTS idx_categories_organization;
DROP INDEX IF EXISTS idx_folders_organization;

ALTER TABLE public.tags       DROP CONSTRAINT IF EXISTS tags_organization_id_fkey;
ALTER TABLE public.categories DROP CONSTRAINT IF EXISTS categories_organization_id_fkey;
ALTER TABLE public.folders    DROP CONSTRAINT IF EXISTS folders_organization_id_fkey;

ALTER TABLE public.tags       DROP COLUMN IF EXISTS organization_id;
ALTER TABLE public.categories DROP COLUMN IF EXISTS organization_id;
ALTER TABLE public.folders    DROP COLUMN IF EXISTS organization_id;

ALTER TABLE public.tags       ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE public.categories ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE public.folders    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE public.tags       DROP CONSTRAINT IF EXISTS tags_user_id_fkey;
ALTER TABLE public.categories DROP CONSTRAINT IF EXISTS categories_user_id_fkey;
ALTER TABLE public.folders    DROP CONSTRAINT IF EXISTS folders_user_id_fkey;

ALTER TABLE public.tags
    ADD CONSTRAINT tags_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE public.categories
    ADD CONSTRAINT categories_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;
ALTER TABLE public.folders
    ADD CONSTRAINT folders_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

COMMENT ON COLUMN public.tags.user_id IS NULL;
COMMENT ON COLUMN public.categories.user_id IS NULL;
COMMENT ON COLUMN public.folders.user_id IS NULL;
COMMENT ON COLUMN public.unibox_thread_labels.user_id IS NULL;

COMMIT;
