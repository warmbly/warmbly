-- Separate user-complaint warmup events (recipient marked the message as
-- spam) from spam-placement events (message landed in the junk folder on
-- delivery without any user action). Previously both were lumped under
-- 'spam_folder' which made it impossible to apply different thresholds.

-- Normalize existing rows to user_complaint — the only producer prior to
-- this migration is event_flags_update.go, which fires on user spam-flag
-- additions. Real placement events start being recorded after this migration.
UPDATE warmup_spam_reports
SET report_type = 'user_complaint'
WHERE report_type IN ('spam', 'spam_folder');

-- Documented set of report types (text-typed column, no enum). Reference for
-- writers:
--   user_complaint   — recipient explicitly flagged the warmup mail as spam
--   spam_placement   — mail landed in the recipient's Junk/Spam folder on arrival
CREATE INDEX IF NOT EXISTS idx_warmup_spam_reports_type_created
    ON warmup_spam_reports (report_type, created_at);
