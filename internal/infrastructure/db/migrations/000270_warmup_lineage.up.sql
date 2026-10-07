ALTER TABLE warmup_tasks
    ADD COLUMN parent_task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    ADD COLUMN parent_received_id uuid,
    ADD COLUMN parent_message_id text,
    ADD COLUMN lineage_version integer,
    ADD COLUMN subject text,
    ADD COLUMN reference_ids text[],
    ADD COLUMN scenario_version text,
    ADD COLUMN rendering_version text,
    ADD COLUMN max_turns integer CHECK (max_turns BETWEEN 1 AND 6),
    ADD COLUMN schedule_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN queued_revision bigint NOT NULL DEFAULT 0;
ALTER TABLE warmup_tasks ALTER COLUMN schedule_revision SET DEFAULT 1;
CREATE UNIQUE INDEX warmup_successor_parent ON warmup_tasks(parent_task_id) WHERE parent_task_id IS NOT NULL;
ALTER TABLE warmup_received
    ADD COLUMN task_id uuid REFERENCES tasks(id) ON DELETE SET NULL,
    ADD COLUMN provider_thread_id text;
CREATE INDEX warmup_received_task ON warmup_received(task_id) WHERE task_id IS NOT NULL;
