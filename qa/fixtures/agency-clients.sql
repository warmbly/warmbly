-- Proof fixture for the client side of flows/agency.flow.ts, applied after fixtures/agency.sql.
-- Gives Northwind Growth a brand, a portal theme and a verified portal at portal.localhost, and adds Casey Client,
-- the client's own teammate in Dev's Organization (not agency staff). Re-runnable.
BEGIN;
DO $$
BEGIN
  IF left(current_database(), 11) <> 'warmbly_qa_' THEN
    RAISE EXCEPTION 'agency proof fixtures require an isolated warmbly_qa_ database';
  END IF;
END $$;

-- The portal theme: a split sign-in page on the accent colour, a geometric font, round corners and the agency's own links.
INSERT INTO agency_brands (agency_id, display_name, accent_color, website_url, portal_domain, portal_domain_verified, portal_domain_verified_at, theme)
VALUES ('a9e00000-0000-0000-0000-000000000001', 'Northwind Growth', '#0f766e', 'https://northwindgrowth.example',
        'portal.localhost', true, now(),
        '{"font":"geometric","corners":"round","sign_in_layout":"split","sign_in_background":"accent",
          "sign_in_heading":"Welcome back to Northwind Growth","sign_in_message":"Your campaigns, replies and reports in one place.",
          "support_email":"help@northwindgrowth.example","support_url":"https://northwindgrowth.example/help",
          "terms_url":"https://northwindgrowth.example/terms","privacy_url":"https://northwindgrowth.example/privacy"}'::jsonb)
ON CONFLICT (agency_id) DO UPDATE SET display_name = EXCLUDED.display_name, accent_color = EXCLUDED.accent_color,
    website_url = EXCLUDED.website_url, portal_domain = EXCLUDED.portal_domain,
    portal_domain_verified = true, portal_domain_verified_at = now(), theme = EXCLUDED.theme;

-- Same password as the seeded owner (password123); onboarding already done.
INSERT INTO users (id, first_name, last_name, email, password_hash, onboarding_completed_at, product_tour_completed_at)
SELECT '86200000-0000-0000-0000-000000000001', 'Casey', 'Client', 'casey@client.example', password_hash, now(), now()
FROM users WHERE email = 'dev@warmbly.com'
ON CONFLICT (id) DO UPDATE SET product_tour_completed_at = coalesce(users.product_tour_completed_at, now());

-- A client-side role: campaigns, contacts, mailboxes, analytics, and the inbox; no team, billing or settings.
INSERT INTO organization_roles (id, organization_id, name, description, color, permissions)
VALUES ('86200000-0000-0000-0000-000000000002', '22222222-0000-0000-0000-000000000001', 'Client team',
        'The client''s own people: run campaigns and read results.', '#0f766e', 19964)
ON CONFLICT (id) DO NOTHING;
INSERT INTO organization_members (organization_id, user_id, role, role_id, permissions, invited_by, invited_at, accepted_at)
VALUES ('22222222-0000-0000-0000-000000000001', '86200000-0000-0000-0000-000000000001', 'Client team',
        '86200000-0000-0000-0000-000000000002', 19964, '11111111-0000-0000-0000-000000000001', now(), now())
ON CONFLICT (organization_id, user_id) DO UPDATE SET role = 'Client team', role_id = EXCLUDED.role_id, permissions = 19964;
INSERT INTO organization_member_roles (organization_id, user_id, role_id)
VALUES ('22222222-0000-0000-0000-000000000001', '86200000-0000-0000-0000-000000000001', '86200000-0000-0000-0000-000000000002')
ON CONFLICT DO NOTHING;
COMMIT;
