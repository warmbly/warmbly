-- Alone in its file for CONCURRENTLY. Finds a unibox message's reply intents without scanning the workspace.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_reply_intents_org_unibox_message
    ON public.reply_intents (organization_id, (metadata->>'unibox_message_id'))
    WHERE (metadata->>'unibox_message_id') IS NOT NULL;
