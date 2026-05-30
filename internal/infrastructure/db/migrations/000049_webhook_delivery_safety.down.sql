DROP INDEX IF EXISTS idx_webhook_deliveries_endpoint_event;

DROP INDEX IF EXISTS idx_webhook_deliveries_due;

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_due
    ON webhook_deliveries (next_attempt_at)
    WHERE status IN ('pending', 'retry');
