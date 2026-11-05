-- Warmup copy proof data for the isolated rich-seed QA database, never production.
-- Every address is on the reserved .example TLD and every send is synthetic: a
-- variant and an own-thread step on the seeded "RevOps outreach - July"
-- campaign, one warmup template, three pool peers and the receipts they filed.
BEGIN;
DO $$
BEGIN
  IF left(current_database(), 11) <> 'warmbly_qa_' THEN
    RAISE EXCEPTION 'warmup copy proof fixtures require an isolated warmbly_qa_ database';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM sequences WHERE id = 'dddd0e00-0000-0000-0000-000000000001') THEN
    RAISE EXCEPTION 'warmup copy proof fixtures require the rich QA seed';
  END IF;
END $$;

-- Re-runnable: clear what an earlier run left.
DELETE FROM warmup_received WHERE message_id LIKE '<wc-%@fixture.example>';
DELETE FROM warmup_tokens WHERE sent_message_id LIKE '<wc-%@fixture.example>';
DELETE FROM tasks WHERE id::text LIKE '86400000-%';
DELETE FROM warmup_conversations WHERE source = 'template' AND organization_id = '22222222-0000-0000-0000-000000000001';
DELETE FROM campaign_ab_variants WHERE id = '86300000-0000-0000-0000-000000000001';
DELETE FROM sequences WHERE id = '86300000-0000-0000-0000-000000000003';
DELETE FROM email_accounts WHERE organization_id = '86300000-0000-0000-0000-0000000000a1';
DELETE FROM organizations WHERE id = '86300000-0000-0000-0000-0000000000a1';
DELETE FROM users WHERE id = '86300000-0000-0000-0000-0000000000b1';

-- A second arm on the entry step, and a step that opens its own thread but asks AI to write a line per lead.
INSERT INTO campaign_ab_variants (id, campaign_id, sequence_id, name, weight, subject, body_plain, is_active)
VALUES ('86300000-0000-0000-0000-000000000001', 'dddd0d00-0000-0000-0000-000000000001', 'dddd0e00-0000-0000-0000-000000000001',
        'Shorter ask', 100, 'Outbound at {{.Company}}',
        E'Hi {{.FirstName}},\n\nHow is {{.Company}} keeping track of which mailboxes land in spam?\n\nWorth a two-minute look?\n\nDev', true);
INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind, thread_reply, created_at)
VALUES ('86300000-0000-0000-0000-000000000003', 'dddd0d00-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
        'Step 4 - new angle', 'A different question for {{.FirstName}}',
        E'Hi {{.FirstName}},\n\n[[ai:icebreaker]]\n\nDev', '', 0, 1, 'email', false, NOW());

-- A workspace template.
INSERT INTO warmup_conversations (id, pool_type, segment, source, theme, subject, description, messages, status, lint_passed, reply_eligible,
                                  scenario_version, rendering_version, semantic_review, organization_id)
VALUES ('86300000-0000-0000-0000-000000000002', 'premium', '', 'template', 'Q4 intro copy', 'Quick question about your Q4 plans',
        E'Hi there,\n\nSaw your team is hiring for sales. Are you still comparing outbound tools this quarter?\n\nDev', '[]', 'active', true, true,
        'natural-v1', 'canonical-v1', 'passed', '22222222-0000-0000-0000-000000000001');

-- Pool peers in a workspace of their own, one per provider group.
INSERT INTO users (id, email, first_name, last_name, password_hash)
VALUES ('86300000-0000-0000-0000-0000000000b1', 'peers@pool-peers.example', 'Pool', 'Peers', 'x');
INSERT INTO organizations (id, name, slug, owner_user_id)
VALUES ('86300000-0000-0000-0000-0000000000a1', 'Pool peers', 'wc-pool-peers', '86300000-0000-0000-0000-0000000000b1');
INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone, mail_host)
VALUES ('86300000-0000-0000-0000-0000000000c1', '86300000-0000-0000-0000-0000000000b1', '86300000-0000-0000-0000-0000000000a1', 'priya@northridge.example', 'Priya Raman', '', '', 'smtp_imap', 'active', 50, 600, 'UTC', 'google_workspace'),
       ('86300000-0000-0000-0000-0000000000c2', '86300000-0000-0000-0000-0000000000b1', '86300000-0000-0000-0000-0000000000a1', 'tom@harbourline.example', 'Tom Okafor', '', '', 'smtp_imap', 'active', 50, 600, 'UTC', 'microsoft365'),
       ('86300000-0000-0000-0000-0000000000c3', '86300000-0000-0000-0000-0000000000b1', '86300000-0000-0000-0000-0000000000a1', 'lena@fjordmail.example', 'Lena Berg', '', '', 'smtp_imap', 'active', 50, 600, 'UTC', 'other');

-- Sends by arm and recipient group, and where each landed. "none" has no receipt yet.
CREATE TEMP TABLE wc_sends ON COMMIT DROP AS
WITH spec(arm, rec, outcome, n) AS (VALUES
  ('step', 'c1', 'inbox', 10), ('step', 'c1', 'tabs', 1), ('step', 'c1', 'spam_rescued', 1), ('step', 'c1', 'none', 2),
  ('step', 'c2', 'inbox', 6), ('step', 'c2', 'spam_rescued', 2), ('step', 'c2', 'spam', 1),
  ('step', 'c3', 'inbox', 3),
  ('variant', 'c1', 'inbox', 7), ('variant', 'c1', 'spam_rescued', 1),
  ('variant', 'c2', 'inbox', 7),
  ('template', 'c1', 'inbox', 5), ('template', 'c2', 'inbox', 4), ('template', 'c2', 'spam_rescued', 1)
), expanded AS (
  SELECT spec.*, row_number() OVER () AS k FROM spec, generate_series(1, spec.n)
)
SELECT ('86400000-0000-0000-0000-' || lpad(k::text, 12, '0'))::uuid AS task_id,
       '<wc-' || k || '@fixture.example>' AS message_id,
       ('33333333-0000-0000-0000-0000dddd000' || (1 + k % 4))::uuid AS sender,
       ('86300000-0000-0000-0000-0000000000' || rec)::uuid AS recipient,
       arm, outcome,
       NOW() - ((k % 9) + 1) * INTERVAL '1 day' - (k % 7) * INTERVAL '1 hour' AS at
FROM expanded;

INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
SELECT task_id, 'warmup', sender, 'completed', message_id FROM wc_sends;

INSERT INTO warmup_tokens (task_id, sender_account_id, recipient_account_id, content_source, conversation_id, sequence_id, variant_id, sent_message_id, subject, created_at)
SELECT task_id, sender, recipient,
       CASE WHEN arm = 'template' THEN 'template' ELSE 'campaign' END,
       CASE WHEN arm = 'template' THEN '86300000-0000-0000-0000-000000000002'::uuid END,
       CASE WHEN arm <> 'template' THEN 'dddd0e00-0000-0000-0000-000000000001'::uuid END,
       CASE WHEN arm = 'variant' THEN '86300000-0000-0000-0000-000000000001'::uuid END,
       message_id, 'Synthetic proof send', at
FROM wc_sends;

INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id, first_landing, landed_spam, rescue_outcome, created_at)
SELECT recipient, gen_random_uuid(), message_id, sender,
       CASE WHEN outcome LIKE 'spam%' THEN 'spam' ELSE outcome END,
       outcome LIKE 'spam%',
       CASE WHEN outcome = 'spam_rescued' THEN 'moved' END,
       at + INTERVAL '2 minutes'
FROM wc_sends WHERE outcome <> 'none';

-- The product tour would cover the page; the flow records warmup copy, not the tour.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;

COMMIT;
