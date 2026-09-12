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

-- Split pass: a user in two workspaces saw one label list, so the same row can
-- be attached to records in both. It can only land in one of them, and the
-- reference from the other would then point across a tenancy boundary: the
-- chip renders as nothing because the other workspace never lists the label,
-- and a title from a foreign workspace is readable where it is joined without
-- a scope check. Each extra workspace gets its own copy of the label instead,
-- and its own rows are repointed at it, so nothing is lost and nothing dangles.
--
-- ON COMMIT DROP: this whole migration is one transaction.
CREATE TEMP TABLE label_split (registry text, original_id uuid, org uuid, new_id uuid) ON COMMIT DROP;

WITH usage AS (
    SELECT et.tag_id AS id, ea.organization_id AS org
    FROM public.email_tags et
    JOIN public.email_accounts ea ON ea.id = et.email_id
    WHERE ea.organization_id IS NOT NULL
    UNION
    SELECT cet.tag_id, c.organization_id
    FROM public.campaign_email_tags cet
    JOIN public.campaigns c ON c.id = cet.campaign_id
    WHERE c.organization_id IS NOT NULL
),
keep AS (
    SELECT id, (array_agg(org ORDER BY org))[1] AS org
    FROM usage GROUP BY id HAVING COUNT(*) > 1
)
INSERT INTO label_split
SELECT 'tags', u.id, u.org, gen_random_uuid()
FROM usage u JOIN keep k ON k.id = u.id AND u.org <> k.org;

WITH usage AS (
    SELECT cc.category_id AS id, c.organization_id AS org
    FROM public.contact_categories cc
    JOIN public.contacts c ON c.id = cc.contact_id
    WHERE c.organization_id IS NOT NULL
    UNION
    SELECT fc.category_id, f.organization_id
    FROM public.form_categories fc
    JOIN public.forms f ON f.id = fc.form_id
),
keep AS (
    SELECT id, (array_agg(org ORDER BY org))[1] AS org
    FROM usage GROUP BY id HAVING COUNT(*) > 1
)
INSERT INTO label_split
SELECT 'categories', u.id, u.org, gen_random_uuid()
FROM usage u JOIN keep k ON k.id = u.id AND u.org <> k.org;

WITH usage AS (
    SELECT cf.folder_id AS id, c.organization_id AS org
    FROM public.campaign_folders cf
    JOIN public.campaigns c ON c.id = cf.campaign_id
    WHERE c.organization_id IS NOT NULL
),
keep AS (
    SELECT id, (array_agg(org ORDER BY org))[1] AS org
    FROM usage GROUP BY id HAVING COUNT(*) > 1
)
INSERT INTO label_split
SELECT 'folders', u.id, u.org, gen_random_uuid()
FROM usage u JOIN keep k ON k.id = u.id AND u.org <> k.org;

-- The copies carry the same name and colour, so the workspace that loses the
-- original sees no change at all.
INSERT INTO public.tags (id, organization_id, user_id, title, color, "position", created_at, updated_at)
SELECT s.new_id, s.org, t.user_id, t.title, t.color, t."position", t.created_at, now()
FROM label_split s JOIN public.tags t ON t.id = s.original_id WHERE s.registry = 'tags';

INSERT INTO public.categories (id, organization_id, user_id, title, color, "position", created_at, updated_at)
SELECT s.new_id, s.org, c.user_id, c.title, c.color, c."position", c.created_at, now()
FROM label_split s JOIN public.categories c ON c.id = s.original_id WHERE s.registry = 'categories';

INSERT INTO public.folders (id, organization_id, user_id, title, color, "position", created_at, updated_at)
SELECT s.new_id, s.org, f.user_id, f.title, f.color, f."position", f.created_at, now()
FROM label_split s JOIN public.folders f ON f.id = s.original_id WHERE s.registry = 'folders';

UPDATE public.email_tags et SET tag_id = s.new_id
FROM label_split s, public.email_accounts ea
WHERE s.registry = 'tags' AND et.tag_id = s.original_id
  AND ea.id = et.email_id AND ea.organization_id = s.org;

UPDATE public.campaign_email_tags cet SET tag_id = s.new_id
FROM label_split s, public.campaigns c
WHERE s.registry = 'tags' AND cet.tag_id = s.original_id
  AND c.id = cet.campaign_id AND c.organization_id = s.org;

UPDATE public.contact_categories cc SET category_id = s.new_id
FROM label_split s, public.contacts c
WHERE s.registry = 'categories' AND cc.category_id = s.original_id
  AND c.id = cc.contact_id AND c.organization_id = s.org;

UPDATE public.form_categories fc SET category_id = s.new_id
FROM label_split s, public.forms f
WHERE s.registry = 'categories' AND fc.category_id = s.original_id
  AND f.id = fc.form_id AND f.organization_id = s.org;

UPDATE public.campaign_folders cf SET folder_id = s.new_id
FROM label_split s, public.campaigns c
WHERE s.registry = 'folders' AND cf.folder_id = s.original_id
  AND c.id = cf.campaign_id AND c.organization_id = s.org;

-- Backfill pass 1: where the label is already attached to something, that
-- something names the workspace it belongs to. After the split every used
-- label points at exactly one organization, so this now claims all of them.
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

-- The thread the label hangs on is the authoritative source: it belongs to a
-- mailbox, and the mailbox names the workspace. Matching on the labeller's own
-- mailboxes keeps it deterministic when a provider thread id appears twice.
UPDATE public.unibox_thread_labels utl
SET organization_id = t.org
FROM (
    SELECT DISTINCT ue.user_id, ue.thread_id, ea.organization_id AS org
    FROM public.unibox_emails ue
    JOIN public.email_accounts ea ON ea.id = ue.email_id
    WHERE ea.organization_id IS NOT NULL AND ue.thread_id <> ''
) t
WHERE utl.thread_id = t.thread_id AND utl.user_id = t.user_id AND utl.organization_id IS NULL;

-- A label on a thread whose messages are gone falls back to its category.
UPDATE public.unibox_thread_labels utl
SET organization_id = c.organization_id
FROM public.categories c
WHERE c.id = utl.category_id AND utl.organization_id IS NULL;

DELETE FROM public.unibox_thread_labels WHERE organization_id IS NULL;

-- Follow the split: a thread in this workspace must point at this workspace's
-- copy of the category, never at the one that stayed behind in another.
UPDATE public.unibox_thread_labels utl
SET category_id = s.new_id
FROM label_split s
WHERE s.registry = 'categories'
  AND utl.category_id = s.original_id
  AND utl.organization_id = s.org;

-- Anything still pointing across a boundary is unreachable from its own
-- workspace and would show that workspace a foreign category's name.
DELETE FROM public.unibox_thread_labels utl
USING public.categories c
WHERE c.id = utl.category_id AND c.organization_id <> utl.organization_id;

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
