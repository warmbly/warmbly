-- Review tab proof: turns the rich seed's "Beta Cold Outreach Q1" into a
-- campaign worth reviewing, in dev@warmbly.com's workspace. Synthetic people on .test domains only. Apply to
-- this worktree's own stack database, never anything shared or real.
BEGIN;

-- Into the signed-in account's workspace, under a clearer name.
UPDATE campaigns SET organization_id = '22222222-0000-0000-0000-000000000001', name = 'Founders outreach - Q4'
WHERE id = '44444444-0000-0000-0000-000000000001';
UPDATE sequences SET organization_id = '22222222-0000-0000-0000-000000000001'
WHERE campaign_id = '44444444-0000-0000-0000-000000000001';
UPDATE contacts SET organization_id = '22222222-0000-0000-0000-000000000001'
WHERE id IN (SELECT contact_id FROM campaign_leads WHERE campaign_id = '44444444-0000-0000-0000-000000000001');

UPDATE sequences SET position = 1, name = 'Intro',
    subject = '{Quick question|A thought} for {{.Company}}',
    body_html = '<p>Hi {{.FirstName}},</p><p><span data-ai-var="opener" data-ai-config="eyJtb2RlIjoiaW5zdGFudCIsInByb21wdCI6Ik9uZSBmcmllbmRseSBsaW5lIGFib3V0IHNvbWV0aGluZyBzcGVjaWZpYyB0byB7ey5Db21wYW55fX0iLCJuYW1lIjoiT3BlbmVyIiwidG9uZSI6Indhcm0ifQ==">[[ai:opener]]</span></p><p>We help teams like {{.Company}} {cut the time spent chasing replies|stop losing warm replies in shared inboxes}. Worth a 15 minute look next week?</p><p>Best,<br>Sam</p>',
    body_plain = ''
WHERE id = '55555555-0000-0000-0000-000000000001';

UPDATE sequences SET position = 2, name = 'Follow-up', subject = '',
    body_html = '<p>Hi {{.FirstName | default "there"}}, just bumping this in case it got buried. Happy to send a two minute video instead if that is easier.</p>',
    body_plain = ''
WHERE id = '55555555-0000-0000-0000-000000000002';

-- Realistic names and companies; two leads with no first name.
UPDATE contacts ct SET first_name = v.first, last_name = v.last, company = v.company, email = v.email
FROM (VALUES
    ('lead1@prospect.test',  'Ana',    'Ruiz',      'Northwind Analytics', 'ana.ruiz@northwind.test'),
    ('lead2@prospect.test',  'Jonas',  'Weber',     'Acme Robotics',       'jonas@acme-robotics.test'),
    ('lead3@prospect.test',  '',       'Okafor',    'Brightline Health',   'ops@brightline.test'),
    ('lead4@prospect.test',  'Priya',  'Natarajan', 'Lumen Logistics',     'priya@lumenlogistics.test'),
    ('lead5@prospect.test',  'Marco',  'Bianchi',   'Fiore Studio',        'marco@fiorestudio.test'),
    ('lead6@prospect.test',  'Sofia',  'Lindqvist', 'Nordhavn Freight',    'sofia@nordhavn.test'),
    ('lead7@prospect.test',  '',       'Tanaka',    'Kumo Cloud',          'hello@kumocloud.test'),
    ('lead8@prospect.test',  'Daniel', 'Moore',     'Harbor Legal',        'daniel@harborlegal.test'),
    ('lead9@prospect.test',  'Leila',  'Haddad',    'Cedar Finance',       'leila@cedarfinance.test'),
    ('lead10@prospect.test', 'Tom',    'Fischer',   'Alpenblick Hotels',   'tom@alpenblick.test')
) AS v(old, first, last, company, email)
WHERE ct.email IN (v.old, v.email) AND ct.id IN (SELECT contact_id FROM campaign_leads WHERE campaign_id = '44444444-0000-0000-0000-000000000001');

-- One lead already has an email personalized for them.
INSERT INTO campaign_lead_step_overrides (campaign_id, contact_id, sequence_id, subject, body_html, body_plain)
SELECT cl.campaign_id, cl.contact_id, '55555555-0000-0000-0000-000000000001',
       'Ana, loved the Northwind data talk',
       '<p>Hi Ana,</p><p>Your talk on cleaning up pipeline data stuck with me, especially the part about reply tracking.</p><p>We help teams like Northwind stop losing warm replies in shared inboxes. Worth a 15 minute look next week?</p><p>Best,<br>Sam</p>',
       ''
FROM campaign_leads cl JOIN contacts ct ON ct.id = cl.contact_id
WHERE cl.campaign_id = '44444444-0000-0000-0000-000000000001' AND ct.email = 'ana.ruiz@northwind.test'
ON CONFLICT DO NOTHING;

-- A draft about to launch, with a clean, verified list.
UPDATE campaigns SET status = 'draft' WHERE id = '44444444-0000-0000-0000-000000000001';
UPDATE contacts SET verification_status = 'valid', subscribed = true
WHERE id IN (SELECT contact_id FROM campaign_leads WHERE campaign_id = '44444444-0000-0000-0000-000000000001');

-- Start from review off with nobody waiting, so the flow can turn it on.
UPDATE campaigns SET review_leads = false WHERE id = '44444444-0000-0000-0000-000000000001';
UPDATE campaign_leads
SET review_pending_at = NULL,
    paused_at = CASE WHEN pause_source = 'review' THEN NULL ELSE paused_at END,
    pause_source = CASE WHEN pause_source = 'review' THEN NULL ELSE pause_source END
WHERE campaign_id = '44444444-0000-0000-0000-000000000001';

COMMIT;
