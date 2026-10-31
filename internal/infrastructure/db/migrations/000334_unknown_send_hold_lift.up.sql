-- Marks a started manual, placement or Cloud send whose outcome stays unknown as no longer holding its mailbox; the send itself can never run again.
ALTER TABLE tasks ADD COLUMN send_hold_lifted_at timestamptz,
    ADD CONSTRAINT tasks_send_hold_lifted_check CHECK (
        send_hold_lifted_at IS NULL OR task_type IN ('email','placement','cloud_warmup')) NOT VALID;
