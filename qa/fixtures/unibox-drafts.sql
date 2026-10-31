-- Apply only to this worktree's isolated rich-seed database.
BEGIN;

UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001'
  AND product_tour_completed_at IS NULL;

UPDATE unibox_emails
SET folder = 'drafts', subject = 'Partnership follow-up draft'
WHERE user_id = '11111111-0000-0000-0000-000000000001'
  AND message_id = '<dev-jonas-2@warmbly.test>';

INSERT INTO ai_thread_drafts (
    id, organization_id, email_account_id, owner_user_id, thread_id,
    source_message_id, to_addr, subject, in_reply_to, body, status
)
SELECT '10870000-0000-0000-0000-000000000001', a.organization_id,
       e.email_id, e.user_id, e.thread_id, e.id,
       'mira.kovacs@lumina-labs.test', e.subject, e.message_id,
       'Hi Mira, we can hold that price through the end of the month. Happy to walk through the report with your COO.',
       'pending'
FROM unibox_emails e
JOIN email_accounts a ON a.id = e.email_id
WHERE e.user_id = '11111111-0000-0000-0000-000000000001'
  AND e.message_id = '<dev-mira-3@lumina-labs.test>'
ON CONFLICT (id) DO UPDATE SET status = 'pending', body = EXCLUDED.body;

COMMIT;
