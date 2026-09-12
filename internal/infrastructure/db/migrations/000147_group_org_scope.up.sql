-- Tags, categories and folders belong to the workspace, not to whoever typed
-- them in.
--
-- All three label registries keyed on user_id, so a tag the owner created was
-- invisible to every teammate: the mailbox list still carried the tag ids, but
-- /auth/me only listed the caller's own rows, so the chips rendered as nothing
-- (issue #436). The same held for contact categories and campaign folders, and
-- for conversation labels in the shared inbox, where one member's labelling was
-- unreadable to the next.
--
-- A user may also belong to several organizations, so a per-user registry
-- leaked one workspace's vocabulary into another. organization_id fixes both.
--
-- user_id stays as the creator, for attribution only, and no longer cascades a
-- delete: offboarding the person who made a tag must not take the workspace's
-- tag with them.

BEGIN;

ALTER TABLE public.tags       ADD COLUMN IF NOT EXISTS organization_id uuid;
ALTER TABLE public.categories ADD COLUMN IF NOT EXISTS organization_id uuid;
ALTER TABLE public.folders    ADD COLUMN IF NOT EXISTS organization_id uuid;

-- Backfill pass 1: where the label is already attached to something, that
-- something names the workspace it belongs to. Only unambiguous cases are
-- taken (every use points at one organization), which is what makes this safe
-- for a user who is a member of more than one.
UPDATE public.tags t
SET organization_id = u.org
FROM (
    -- (array_agg(DISTINCT ...))[1] because Postgres has no min(uuid); HAVING
    -- has already reduced the group to one distinct value anyway.
    SELECT x.tag_id, (array_agg(DISTINCT x.org))[1] AS org
    FROM (
        SELECT et.tag_id, ea.organization_id AS org
        FROM public.email_tags et
        JOIN public.email_accounts ea ON ea.id = et.email_id
        WHERE ea.organization_id IS NOT NULL
        UNION ALL
        SELECT cet.tag_id, c.organization_id
        FROM public.campaign_email_tags cet
        JOIN public.campaigns c ON c.id = cet.campaign_id
        WHERE c.organization_id IS NOT NULL
    ) x
    GROUP BY x.tag_id
    HAVING COUNT(DISTINCT x.org) = 1
) u
WHERE t.id = u.tag_id;

UPDATE public.categories cat
SET organization_id = u.org
FROM (
    SELECT cc.category_id, (array_agg(DISTINCT c.organization_id))[1] AS org
    FROM public.contact_categories cc
    JOIN public.contacts c ON c.id = cc.contact_id
    WHERE c.organization_id IS NOT NULL
    GROUP BY cc.category_id
    HAVING COUNT(DISTINCT c.organization_id) = 1
) u
WHERE cat.id = u.category_id;

UPDATE public.folders f
SET organization_id = u.org
FROM (
    SELECT cf.folder_id, (array_agg(DISTINCT c.organization_id))[1] AS org
    FROM public.campaign_folders cf
    JOIN public.campaigns c ON c.id = cf.campaign_id
    WHERE c.organization_id IS NOT NULL
    GROUP BY cf.folder_id
    HAVING COUNT(DISTINCT c.organization_id) = 1
) u
WHERE f.id = u.folder_id;

-- Backfill pass 2: an unused label has nothing to point at, so it follows its
-- creator — the organization they own, else the first they joined.
UPDATE public.tags SET organization_id = COALESCE(
    (SELECT o.id FROM public.organizations o WHERE o.owner_user_id = tags.user_id ORDER BY o.created_at, o.id LIMIT 1),
    (SELECT m.organization_id FROM public.organization_members m WHERE m.user_id = tags.user_id ORDER BY m.invited_at, m.id LIMIT 1)
) WHERE organization_id IS NULL;

UPDATE public.categories SET organization_id = COALESCE(
    (SELECT o.id FROM public.organizations o WHERE o.owner_user_id = categories.user_id ORDER BY o.created_at, o.id LIMIT 1),
    (SELECT m.organization_id FROM public.organization_members m WHERE m.user_id = categories.user_id ORDER BY m.invited_at, m.id LIMIT 1)
) WHERE organization_id IS NULL;

UPDATE public.folders SET organization_id = COALESCE(
    (SELECT o.id FROM public.organizations o WHERE o.owner_user_id = folders.user_id ORDER BY o.created_at, o.id LIMIT 1),
    (SELECT m.organization_id FROM public.organization_members m WHERE m.user_id = folders.user_id ORDER BY m.invited_at, m.id LIMIT 1)
) WHERE organization_id IS NULL;

-- A label whose creator belongs to no organization at all is reachable from
-- nowhere once the registry is workspace-scoped.
DELETE FROM public.tags       WHERE organization_id IS NULL;
DELETE FROM public.categories WHERE organization_id IS NULL;
DELETE FROM public.folders    WHERE organization_id IS NULL;

ALTER TABLE public.tags       ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE public.categories ALTER COLUMN organization_id SET NOT NULL;
ALTER TABLE public.folders    ALTER COLUMN organization_id SET NOT NULL;

ALTER TABLE public.tags
    ADD CONSTRAINT tags_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;
ALTER TABLE public.categories
    ADD CONSTRAINT categories_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;
ALTER TABLE public.folders
    ADD CONSTRAINT folders_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

-- Positions were per user and are now per workspace, so two members' rows
-- collided at 0, 1, 2. Renumber each registry once, keeping the existing order
-- and breaking ties by age. Titles are deliberately NOT merged: two members who
-- each made a "Prospects" tag made two tags, and collapsing them would silently
-- retag their data.
WITH renumbered AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY organization_id ORDER BY "position", created_at, id) - 1 AS pos
    FROM public.tags
)
UPDATE public.tags t SET "position" = r.pos FROM renumbered r WHERE t.id = r.id AND t."position" <> r.pos;

WITH renumbered AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY organization_id ORDER BY "position", created_at, id) - 1 AS pos
    FROM public.categories
)
UPDATE public.categories c SET "position" = r.pos FROM renumbered r WHERE c.id = r.id AND c."position" <> r.pos;

WITH renumbered AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY organization_id ORDER BY "position", created_at, id) - 1 AS pos
    FROM public.folders
)
UPDATE public.folders f SET "position" = r.pos FROM renumbered r WHERE f.id = r.id AND f."position" <> r.pos;

CREATE INDEX IF NOT EXISTS idx_tags_organization       ON public.tags       USING btree (organization_id, "position");
CREATE INDEX IF NOT EXISTS idx_categories_organization ON public.categories USING btree (organization_id, "position");
CREATE INDEX IF NOT EXISTS idx_folders_organization    ON public.folders    USING btree (organization_id, "position");

-- user_id is attribution now. Deleting the creator must not delete the
-- workspace's label, so the cascade becomes SET NULL and the column nullable.
ALTER TABLE public.tags       DROP CONSTRAINT IF EXISTS tags_user_id_fkey;
ALTER TABLE public.categories DROP CONSTRAINT IF EXISTS categories_user_id_fkey;
ALTER TABLE public.folders    DROP CONSTRAINT IF EXISTS folders_user_id_fkey;

ALTER TABLE public.tags       ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE public.categories ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE public.folders    ALTER COLUMN user_id DROP NOT NULL;

ALTER TABLE public.tags
    ADD CONSTRAINT tags_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;
ALTER TABLE public.categories
    ADD CONSTRAINT categories_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;
ALTER TABLE public.folders
    ADD CONSTRAINT folders_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;

COMMENT ON COLUMN public.tags.user_id       IS 'Who created it. Attribution only; the tag belongs to organization_id.';
COMMENT ON COLUMN public.categories.user_id IS 'Who created it. Attribution only; the category belongs to organization_id.';
COMMENT ON COLUMN public.folders.user_id    IS 'Who created it. Attribution only; the folder belongs to organization_id.';

-- ── Conversation labels ────────────────────────────────────────────────
--
-- The unified inbox is already read per organization, so a label that only its
-- author could see made the label rail count threads nobody else could find.
-- It moves to the workspace with the vocabulary it draws on.

ALTER TABLE public.unibox_thread_labels ADD COLUMN IF NOT EXISTS organization_id uuid;

UPDATE public.unibox_thread_labels utl
SET organization_id = c.organization_id
FROM public.categories c
WHERE c.id = utl.category_id AND utl.organization_id IS NULL;

DELETE FROM public.unibox_thread_labels WHERE organization_id IS NULL;

-- Two members who labelled the same thread with the same category collapse to
-- one row under the new key.
DELETE FROM public.unibox_thread_labels a
USING public.unibox_thread_labels b
WHERE a.organization_id = b.organization_id
  AND a.thread_id = b.thread_id
  AND a.category_id = b.category_id
  AND (a.created_at, a.user_id) > (b.created_at, b.user_id);

ALTER TABLE public.unibox_thread_labels ALTER COLUMN organization_id SET NOT NULL;

ALTER TABLE public.unibox_thread_labels DROP CONSTRAINT IF EXISTS unibox_thread_labels_pkey;
ALTER TABLE public.unibox_thread_labels
    ADD CONSTRAINT unibox_thread_labels_pkey PRIMARY KEY (organization_id, thread_id, category_id);

ALTER TABLE public.unibox_thread_labels
    ADD CONSTRAINT unibox_thread_labels_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_unibox_thread_labels_thread;
CREATE INDEX IF NOT EXISTS idx_unibox_thread_labels_org_thread
    ON public.unibox_thread_labels USING btree (organization_id, thread_id);

-- Same rule as the registries: the person who labelled a conversation leaving
-- must not unlabel it.
ALTER TABLE public.unibox_thread_labels DROP CONSTRAINT IF EXISTS unibox_thread_labels_user_id_fkey;
ALTER TABLE public.unibox_thread_labels ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE public.unibox_thread_labels
    ADD CONSTRAINT unibox_thread_labels_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE SET NULL;

COMMENT ON COLUMN public.unibox_thread_labels.user_id IS 'Who applied it. Attribution only; the label belongs to organization_id.';

COMMIT;
