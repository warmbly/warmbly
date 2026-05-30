DROP INDEX IF EXISTS idx_webhook_deliveries_due;

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_due
    ON webhook_deliveries (next_attempt_at)
    WHERE status = 'pending';

DELETE FROM webhook_deliveries a
USING webhook_deliveries b
WHERE a.endpoint_id = b.endpoint_id
  AND a.event_id = b.event_id
  AND a.ctid < b.ctid;

CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint_event
    ON webhook_deliveries (endpoint_id, event_id);
