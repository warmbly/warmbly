ALTER TABLE email_tasks
    DROP COLUMN IF EXISTS forwarded_plain,
    DROP COLUMN IF EXISTS forwarded_html;
