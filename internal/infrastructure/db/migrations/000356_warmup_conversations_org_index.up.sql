-- Alone in its file for CONCURRENTLY. Serves a workspace's template listing and its template draw.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warmup_conversations_org
    ON public.warmup_conversations (organization_id)
    WHERE organization_id IS NOT NULL;
