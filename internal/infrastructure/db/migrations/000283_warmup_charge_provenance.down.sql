DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM warmup_tasks WHERE warmup_charged_date IS NOT NULL) THEN
        RAISE EXCEPTION 'Cannot remove recorded warmup charge and refund evidence; retain the schema and roll forward';
    END IF;
END $$;

ALTER TABLE warmup_tasks
    DROP CONSTRAINT warmup_tasks_charge_provenance,
    DROP COLUMN warmup_charged_date,
    DROP COLUMN warmup_reply_charged,
    DROP COLUMN warmup_refunded_at;
