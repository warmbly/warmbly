-- Validates the CHECK 000334 added NOT VALID. A VALIDATE only takes a SHARE UPDATE EXCLUSIVE lock, so writes continue while it scans.
ALTER TABLE tasks VALIDATE CONSTRAINT tasks_send_hold_lifted_check;
