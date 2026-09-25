-- Verdicts already stored: the machine kinds, unless the kind itself was below
-- the confidence floor.
UPDATE public.inbox_tag_results
SET automated = true
WHERE status = 'complete'
  AND kind IN ('bounce_hard', 'bounce_soft', 'auto_reply_ooo', 'auto_reply_ticket', 'notification')
  AND NOT (needs_review AND review_reason = 'kind');

UPDATE public.unibox_emails ue
SET automated = true
FROM public.inbox_tag_results r
WHERE r.automated
  AND ue.email_id = r.email_account_id
  AND ue.message_id = r.message_id
  AND NOT ue.automated;

-- Automatic labels move to plain names. A NULL new title is a label that is no
-- longer written: its automatic uses go, and so does the label once nothing
-- else refers to it.
CREATE TEMP TABLE inbox_label_renames (ord int, old text, new text);
INSERT INTO inbox_label_renames (ord, old, new) VALUES
    (1,  'bounce-hard',           'Bounced'),
    (2,  'bounce-soft',           'Bounced'),
    (3,  'auto-reply-ooo',        'Out of office'),
    (4,  'auto-reply-ticket',     'Auto-reply'),
    (5,  'notification',          'Notification'),
    (6,  'cold-inbound',          'Sales pitch'),
    (7,  'agreed',                'Interested'),
    (8,  'scheduling',            'Meeting'),
    (9,  'asks-for-call',         'Meeting'),
    (10, 'wants-pricing',         'Pricing'),
    (11, 'wants-info',            'Question'),
    (12, 'in-progress',           'Update'),
    (13, 'question-answered',     'Update'),
    (14, 'not-now',               'Not now'),
    (15, 'not-interested',        'Not interested'),
    (16, 'wrong-person',          'Wrong person'),
    (17, 'opt-out',               'Unsubscribe'),
    (18, 'requests-removal',      'Unsubscribe'),
    (19, 'legal-threat',          'Legal threat'),
    (20, 'needs-review',          'Needs review'),
    (21, 'ball-in-our-court',     'Needs reply'),
    (22, 'follow-up-due',         'Follow up'),
    (23, 'going-cold',            'Gone quiet'),
    (24, 'human-reply',           NULL),
    (25, 'internal',              NULL),
    (26, 'unclear',               NULL),
    (27, 'needs-human-judgement', NULL),
    (28, 'awaiting-reply',        NULL);

DO $$
DECLARE
    m      record;
    c      record;
    target uuid;
BEGIN
    FOR m IN SELECT * FROM inbox_label_renames ORDER BY ord LOOP
        FOR c IN SELECT id, organization_id FROM public.categories WHERE LOWER(title) = m.old LOOP
            IF m.new IS NULL THEN
                DELETE FROM public.unibox_thread_labels WHERE category_id = c.id AND user_id IS NULL;
            ELSE
                SELECT id INTO target FROM public.categories
                WHERE organization_id = c.organization_id AND LOWER(title) = LOWER(m.new) AND id <> c.id
                ORDER BY "position", id LIMIT 1;

                IF target IS NULL THEN
                    UPDATE public.categories SET title = m.new, updated_at = now() WHERE id = c.id;
                    CONTINUE;
                END IF;

                -- Two old labels now share a name: fold this one into the other.
                INSERT INTO public.unibox_thread_labels (organization_id, thread_id, category_id, user_id, created_at)
                SELECT organization_id, thread_id, target, user_id, created_at
                FROM public.unibox_thread_labels WHERE category_id = c.id
                ON CONFLICT (organization_id, thread_id, category_id) DO NOTHING;
                DELETE FROM public.unibox_thread_labels WHERE category_id = c.id;
            END IF;

            -- Contacts and forms share the category registry; a label they use stays.
            DELETE FROM public.categories cat
            WHERE cat.id = c.id
              AND NOT EXISTS (SELECT 1 FROM public.unibox_thread_labels WHERE category_id = cat.id)
              AND NOT EXISTS (SELECT 1 FROM public.contact_categories WHERE category_id = cat.id)
              AND NOT EXISTS (SELECT 1 FROM public.form_categories WHERE category_id = cat.id);
        END LOOP;
    END LOOP;
END $$;

-- The review page shows what each verdict filed under, so history reads in the
-- same words.
UPDATE public.inbox_tag_results r
SET labels = COALESCE((
    SELECT array_agg(t.label ORDER BY t.pos)
    FROM (
        SELECT COALESCE(m.new, u.label) AS label, MIN(u.pos) AS pos
        FROM unnest(r.labels) WITH ORDINALITY AS u(label, pos)
        LEFT JOIN inbox_label_renames m ON m.old = u.label
        WHERE m.old IS NULL OR m.new IS NOT NULL
        GROUP BY 1
    ) t
), '{}')
WHERE r.labels && (SELECT array_agg(old) FROM inbox_label_renames);

DROP TABLE inbox_label_renames;
