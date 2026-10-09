-- A restricted client teammate for the isolated rich-seed QA database, never production.
BEGIN;
DO $$
BEGIN
  IF left(current_database(), 11) <> 'warmbly_qa_' THEN
    RAISE EXCEPTION 'member access proof fixtures require an isolated warmbly_qa_ database';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM users WHERE email = 'dev@warmbly.com') THEN
    RAISE EXCEPTION 'member access proof fixtures require the rich QA seed';
  END IF;
END $$;

-- Same password as the seeded owner (password123); onboarding already done.
INSERT INTO users (id, first_name, last_name, email, password_hash, onboarding_completed_at)
SELECT '86100000-0000-0000-0000-000000000001', 'Casey', 'Client', 'client@acme-agency.test', password_hash, now()
FROM users WHERE email = 'dev@warmbly.com'
ON CONFLICT (id) DO NOTHING;

INSERT INTO organization_roles (id, organization_id, name, description, color, permissions)
VALUES ('86100000-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001', 'Viewer',
        'Read-only access to campaigns, contacts, reports and the inbox.', '#f59e0b', 3232)
ON CONFLICT (id) DO NOTHING;

-- Restricted from the moment the row exists, so its permissions are masked on insert.
INSERT INTO organization_members (organization_id, user_id, role, role_id, permissions, invited_by, invited_at, accepted_at, access_scope)
VALUES ('22222222-0000-0000-0000-000000000001', '86100000-0000-0000-0000-000000000001', 'Viewer',
        '86100000-0000-0000-0000-000000000002', 3232, '11111111-0000-0000-0000-000000000001', now(), now(), 'restricted')
ON CONFLICT (organization_id, user_id) DO UPDATE SET access_scope = 'restricted';
INSERT INTO organization_member_roles (organization_id, user_id, role_id)
VALUES ('22222222-0000-0000-0000-000000000001', '86100000-0000-0000-0000-000000000001', '86100000-0000-0000-0000-000000000002')
ON CONFLICT DO NOTHING;

-- Their grants: the Outbound folder and the mailbox its campaign replies arrive in.
DELETE FROM organization_member_campaign_access WHERE user_id = '86100000-0000-0000-0000-000000000001';
DELETE FROM organization_member_mailbox_access WHERE user_id = '86100000-0000-0000-0000-000000000001';
INSERT INTO organization_member_campaign_access (organization_id, user_id, folder_id, granted_by)
SELECT f.organization_id, '86100000-0000-0000-0000-000000000001', f.id, '11111111-0000-0000-0000-000000000001'
FROM folders f WHERE f.organization_id = '22222222-0000-0000-0000-000000000001' AND f.title = 'Outbound';
INSERT INTO organization_member_mailbox_access (organization_id, user_id, email_account_id, granted_by)
SELECT ea.organization_id, '86100000-0000-0000-0000-000000000001', ea.id, '11111111-0000-0000-0000-000000000001'
FROM email_accounts ea WHERE ea.organization_id = '22222222-0000-0000-0000-000000000001' AND ea.email = 'dev.outbound@warmbly.test';
COMMIT;
