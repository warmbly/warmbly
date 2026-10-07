DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id
               WHERE w.parent_task_id IS NOT NULL AND t.status IN ('pending','active')) THEN
        RAISE EXCEPTION 'resolve pending diagnostic successors before rollback';
    END IF;
END $$;
DROP INDEX warmup_received_task;
ALTER TABLE warmup_received DROP COLUMN task_id, DROP COLUMN provider_thread_id;
DROP INDEX warmup_successor_parent;
ALTER TABLE warmup_tasks DROP COLUMN parent_task_id, DROP COLUMN parent_received_id,
    DROP COLUMN parent_message_id, DROP COLUMN lineage_version, DROP COLUMN subject,
    DROP COLUMN reference_ids, DROP COLUMN scenario_version, DROP COLUMN rendering_version,
    DROP COLUMN max_turns, DROP COLUMN schedule_revision, DROP COLUMN queued_revision;
