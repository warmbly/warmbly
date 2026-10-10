-- Retain operator evidence when rolling back; restoring the old constraints would discard it.
DROP INDEX IF EXISTS tasks_unapplied_executor_result;
DROP INDEX IF EXISTS warmup_unapplied_dispatch_result;
DROP INDEX IF EXISTS tasks_unreserved_unknown_result;
DROP INDEX IF EXISTS send_recovery_unknown_resolution;
