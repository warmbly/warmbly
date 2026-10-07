ALTER TABLE warmup_conversations
    DROP COLUMN scenario_version,
    DROP COLUMN rendering_version,
    DROP COLUMN semantic_review;

ALTER TABLE warmup_generation_jobs
    DROP COLUMN content_version,
    DROP COLUMN max_messages_per_thread;
