ALTER TABLE warmup_tasks
    ADD COLUMN warmup_charged_date DATE DEFAULT NULL,
    ADD COLUMN warmup_reply_charged BOOLEAN DEFAULT NULL,
    ADD COLUMN warmup_refunded_at TIMESTAMPTZ DEFAULT NULL,
    ADD CONSTRAINT warmup_tasks_charge_provenance CHECK (
        (warmup_charged_date IS NULL AND warmup_reply_charged IS NULL AND warmup_refunded_at IS NULL)
        OR (warmup_charged_date IS NOT NULL AND warmup_reply_charged IS NOT NULL)
    );
