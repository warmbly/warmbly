DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM warmup_tasks WHERE dispatch_nonce IS NOT NULL AND dispatch_result IS NULL) THEN
        RAISE EXCEPTION 'warmup dispatch authority still unresolved';
    END IF;
END $$;
ALTER TABLE warmup_tasks DROP COLUMN dispatch_result, DROP COLUMN dispatch_started_at,
    DROP COLUMN dispatch_worker_id, DROP COLUMN dispatch_nonce;
ALTER TABLE fleet_nodes DROP COLUMN warmup_send_protocol;
