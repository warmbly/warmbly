-- Conversation labels for the Unibox.
--
-- Inbox "categories" reuse the existing per-user `categories` registry
-- (the same generic group table that backs contact categories) but are
-- attached at the CONVERSATION (thread) level rather than per message —
-- so a label tags a whole thread, which lines up with the thread-stacked
-- inbox list. Assignment is independent from contact categorisation: the
-- vocabulary is shared, the join is inbox-specific.
CREATE TABLE IF NOT EXISTS public.unibox_thread_labels (
    user_id     uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    thread_id   text NOT NULL,
    category_id uuid NOT NULL REFERENCES public.categories(id) ON DELETE CASCADE,
    created_at  timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (user_id, thread_id, category_id)
);

-- Counting threads per label (scope rail) reads by category.
CREATE INDEX IF NOT EXISTS idx_unibox_thread_labels_category
    ON public.unibox_thread_labels USING btree (category_id);

-- Aggregating a thread's labels into a list row reads by (user, thread).
CREATE INDEX IF NOT EXISTS idx_unibox_thread_labels_thread
    ON public.unibox_thread_labels USING btree (user_id, thread_id);

-- Thread-collapse query: pick the newest message per thread across every
-- mailbox a user owns. The existing idx_unibox_emails_thread leads with
-- (user_id, email_id, thread_id) which doesn't serve the cross-mailbox
-- "latest per thread" window; this one does.
CREATE INDEX IF NOT EXISTS idx_unibox_emails_user_thread_date
    ON public.unibox_emails USING btree (user_id, thread_id, internal_date DESC);
