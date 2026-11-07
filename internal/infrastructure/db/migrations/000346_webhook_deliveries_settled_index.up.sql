-- Alone in its file for CONCURRENTLY. Serves the delivery-log prune, which only removes settled deliveries.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_webhook_deliveries_settled
    ON public.webhook_deliveries (updated_at)
    WHERE status IN ('delivered', 'failed', 'abandoned');
