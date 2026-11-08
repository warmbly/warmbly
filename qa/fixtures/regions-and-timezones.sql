-- Leads with location fields, a segment and a draft campaign for the isolated rich-seed QA database, never production.
BEGIN;
DO $$
BEGIN
  IF left(current_database(), 11) <> 'warmbly_qa_' THEN
    RAISE EXCEPTION 'region proof fixtures require an isolated warmbly_qa_ database';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = '22222222-0000-0000-0000-000000000001') THEN
    RAISE EXCEPTION 'region proof fixtures require the rich QA seed';
  END IF;
END $$;

-- Synthetic leads on the reserved .example TLD, so no address can ever be delivered to. The
-- location fields come in the shapes real exports use; the backend's sweep places them.
INSERT INTO contacts (id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields)
SELECT ('86300000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid,
       '11111111-0000-0000-0000-000000000001', '22222222-0000-0000-0000-000000000001',
       'Lead', n::text, 'lead' || n || '@prospect' || (n % 60) || '.example', 'Prospect ' || (n % 60), '',
       (ARRAY[
         '{"country":"United States","state":"TX"}',
         '{"country":"United States","state":"California"}',
         '{"location":"New York, NY"}',
         '{"Country/Region":"Germany"}',
         '{"country":"UK"}',
         '{"city":"Tokyo"}',
         '{"timezone":"Australia/Sydney"}',
         '{"country":"Brazil"}',
         '{"timezone":"IST"}',
         '{}'
       ])[1 + n % 10]::jsonb
FROM generate_series(1, 120) AS n
ON CONFLICT DO NOTHING;

INSERT INTO segments (id, organization_id, created_by, name, description, color)
VALUES ('86300000-0000-0000-0000-000000000000', '22222222-0000-0000-0000-000000000001',
        '11111111-0000-0000-0000-000000000001', 'Prospects worldwide', 'Synthetic leads in several regions', '#0284c7')
ON CONFLICT (id) DO NOTHING;

INSERT INTO segment_members (segment_id, contact_id, mode)
SELECT '86300000-0000-0000-0000-000000000000', ('86300000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid, 'include'
FROM generate_series(1, 120) AS n
ON CONFLICT DO NOTHING;

-- A draft weekday 9-5 campaign on New York time holding the same leads.
INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
                       daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
VALUES ('86300000-0000-0000-0000-00000000c001', '11111111-0000-0000-0000-000000000001',
        '22222222-0000-0000-0000-000000000001', 'Worldwide outreach', '', 'draft',
        50, 'America/New_York', 31, '09:00', '17:00', 'least_recently_used', now(), now())
ON CONFLICT (id) DO UPDATE SET timezone_mode = 'campaign';

INSERT INTO campaign_leads (campaign_id, contact_id, position)
SELECT '86300000-0000-0000-0000-00000000c001', ('86300000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid, n
FROM generate_series(1, 120) AS n
ON CONFLICT DO NOTHING;

-- Region segments a previous recording made, so the split creates them again.
DELETE FROM segments WHERE organization_id = '22222222-0000-0000-0000-000000000001' AND name LIKE 'Prospects worldwide · %';

-- The product tour would cover the page.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;
COMMIT;
