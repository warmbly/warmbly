DROP INDEX IF EXISTS idx_email_account_errors_code;
DROP INDEX IF EXISTS idx_email_account_errors_user;
DROP INDEX IF EXISTS idx_email_account_errors_unresolved;
DROP TABLE IF EXISTS email_account_errors;
DROP TYPE IF EXISTS email_error_resolve_method;
DROP TYPE IF EXISTS email_error_severity;
