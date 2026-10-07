ALTER TABLE warmup_conversations
    ADD COLUMN scenario_version text,
    ADD COLUMN rendering_version text,
    ADD COLUMN semantic_review text NOT NULL DEFAULT 'legacy_unknown'
        CHECK (semantic_review IN ('legacy_unknown', 'legacy_unreviewed', 'unreviewed', 'unavailable', 'rejected', 'passed'));

ALTER TABLE warmup_generation_jobs
    ADD COLUMN content_version text,
    ADD COLUMN max_messages_per_thread integer CHECK (max_messages_per_thread BETWEEN 1 AND 5);
