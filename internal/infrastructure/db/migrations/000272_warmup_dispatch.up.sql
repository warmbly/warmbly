ALTER TABLE fleet_nodes ADD COLUMN warmup_send_protocol integer NOT NULL DEFAULT 0;
ALTER TABLE warmup_tasks
    ADD COLUMN dispatch_nonce uuid,
    ADD COLUMN dispatch_worker_id uuid REFERENCES fleet_nodes(id) ON DELETE SET NULL,
    ADD COLUMN dispatch_started_at timestamptz,
    ADD COLUMN dispatch_result jsonb;
