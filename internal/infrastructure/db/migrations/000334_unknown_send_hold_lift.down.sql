DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tasks WHERE send_hold_lifted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot remove lifted unknown send holds; retain the schema and roll forward';
    END IF;
END $$;

ALTER TABLE tasks DROP CONSTRAINT tasks_send_hold_lifted_check,
    DROP COLUMN send_hold_lifted_at;
