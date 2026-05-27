DROP INDEX IF EXISTS idx_warmup_spam_reports_type_created;

UPDATE warmup_spam_reports
SET report_type = 'spam_folder'
WHERE report_type = 'user_complaint';
