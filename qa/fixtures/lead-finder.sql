-- Lead finder leads and a saved search for the isolated rich-seed QA database, never production.
BEGIN;
DO $$
BEGIN
  IF left(current_database(), 11) <> 'warmbly_qa_' THEN
    RAISE EXCEPTION 'lead finder proof fixtures require an isolated warmbly_qa_ database';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM organizations WHERE id = '22222222-0000-0000-0000-000000000001') THEN
    RAISE EXCEPTION 'lead finder proof fixtures require the rich QA seed';
  END IF;
END $$;

DELETE FROM lead_enrichment_runs WHERE organization_id = '22222222-0000-0000-0000-000000000001' AND id::text LIKE '86300000-%';
DELETE FROM lead_searches WHERE organization_id = '22222222-0000-0000-0000-000000000001' AND id::text LIKE '86300000-%';
DELETE FROM leads WHERE organization_id = '22222222-0000-0000-0000-000000000001' AND id::text LIKE '86300000-%';

INSERT INTO lead_searches (id, organization_id, created_by_user_id, name, provider, query, category, location_label, area, saved, result_count, pages_fetched)
VALUES ('86300000-0000-0000-0000-000000000000', '22222222-0000-0000-0000-000000000001', '11111111-0000-0000-0000-000000000001',
        'Dentists in downtown Austin', 'osm', '', 'dentist', 'Austin, Texas',
        '{"kind":"circle","center":{"lat":30.2672,"lng":-97.7431},"radius_m":4500,"bounds":{"south":30.2267,"west":-97.79,"north":30.3077,"east":-97.6962}}',
        true, 36, 1);

-- Synthetic businesses on the reserved .example TLD, so no address or site is real.
WITH names(n, name) AS (
  SELECT n, (ARRAY['Bright Smiles Dental','Congress Avenue Dentistry','Lady Bird Family Dental','Barton Springs Orthodontics',
    'Rainey Street Smiles','Zilker Dental Studio','East Sixth Dental','Clarksville Dental Care','Bouldin Creek Dentistry',
    'Travis Heights Dental','Mueller Family Dentistry','Hyde Park Dental Arts','Riverside Dental Group','South Lamar Smiles',
    'Capitol View Dental','Pease Park Pediatric Dentistry','Shoal Creek Dental','Seaholm Dental Loft','Red River Dental',
    'Cherrywood Dental','Holly Street Dental','Tarrytown Dental Office','Market District Dentistry','Colorado River Dental',
    'Waterloo Dental Spa','Old Pecan Dental','Willow Creek Implants','Guadalupe Dental','Rosewood Dental Clinic',
    'Brazos Street Dental','Lamar Union Dentistry','Saltillo Smiles','Downtown Austin Endodontics','Treaty Oak Dental',
    'Blue Bonnet Dental','Moonlight Tower Dentistry'])[n]
  FROM generate_series(1, 36) AS n
)
INSERT INTO leads (id, organization_id, provider, external_id, name, category, categories, address, street, city, region, postcode, country,
                   latitude, longitude, phone, website, domain, email, emails, socials, rating, review_count, hours, business_status,
                   maps_url, site, status, enrichment_status, enriched_at, tags, notes, first_seen_at, last_seen_at)
SELECT ('86300000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid,
       '22222222-0000-0000-0000-000000000001', 'osm', 'node/86300' || n, name, 'Dentist', ARRAY['Dentist'],
       (100 + n * 7) || ' ' || (ARRAY['Congress Ave','E 6th St','S Lamar Blvd','Guadalupe St','W 5th St','Red River St'])[1 + n % 6] || ', Austin, TX 787' || lpad((n % 40)::text, 2, '0'),
       (100 + n * 7) || ' ' || (ARRAY['Congress Ave','E 6th St','S Lamar Blvd','Guadalupe St','W 5th St','Red River St'])[1 + n % 6],
       'Austin', 'TX', '787' || lpad((n % 40)::text, 2, '0'), 'US',
       30.2672 + 0.028 * sin(n * 12.9898), -97.7431 + 0.034 * cos(n * 78.233),
       CASE WHEN n % 5 = 0 THEN '' ELSE '+1 512 555 0' || lpad(n::text, 3, '0') END,
       CASE WHEN n % 4 = 3 THEN '' ELSE 'https://' || lower(replace(name, ' ', '')) || '.example' END,
       CASE WHEN n % 4 = 3 THEN '' ELSE lower(replace(name, ' ', '')) || '.example' END,
       CASE WHEN n % 4 = 3 OR n % 7 = 0 THEN '' ELSE (ARRAY['info','hello','frontdesk','office'])[1 + n % 4] || '@' || lower(replace(name, ' ', '')) || '.example' END,
       CASE WHEN n % 4 = 3 OR n % 7 = 0 THEN '[]'::jsonb ELSE jsonb_build_array(
         jsonb_build_object('email', (ARRAY['info','hello','frontdesk','office'])[1 + n % 4] || '@' || lower(replace(name, ' ', '')) || '.example',
                            'source', 'website', 'url', 'https://' || lower(replace(name, ' ', '')) || '.example/contact',
                            'domain_match', true, 'status', CASE WHEN n % 6 = 1 THEN 'risky' ELSE 'valid' END),
         jsonb_build_object('email', 'dr.' || lower(split_part(name, ' ', 1)) || '@' || lower(replace(name, ' ', '')) || '.example',
                            'source', 'website', 'url', 'https://' || lower(replace(name, ' ', '')) || '.example/team',
                            'domain_match', true, 'status', 'unknown')) END,
       CASE WHEN n % 3 = 0 THEN '{}'::jsonb ELSE jsonb_build_object('facebook', 'https://www.facebook.com/' || lower(replace(name, ' ', '')),
                                                                       'instagram', 'https://www.instagram.com/' || lower(replace(name, ' ', ''))) END,
       round((3.4 + (n % 17) * 0.095)::numeric, 1)::float8, 8 + (n * 37) % 640,
       '["Mo-Fr 08:00-17:00", "Sa 09:00-13:00"]'::jsonb, 'operational',
       'https://www.openstreetmap.org/node/86300' || n,
       CASE WHEN n % 4 = 3 THEN '{}'::jsonb ELSE jsonb_build_object(
         'reachable', true, 'https', n % 5 <> 2, 'final_url', 'https://' || lower(replace(name, ' ', '')) || '.example/',
         'title', name || ' | Family and cosmetic dentistry in Austin',
         'description', 'Gentle, modern dental care in the heart of Austin. New patients welcome.',
         'platform', (ARRAY['WordPress','Wix','Squarespace','Webflow'])[1 + n % 4],
         'signals', CASE WHEN n % 5 = 2 THEN '["No HTTPS", "Not mobile friendly", "Outdated footer (2019)", "No analytics"]'::jsonb
                         ELSE '["Google Analytics", "Online booking", "Contact form"]'::jsonb END,
         'pages', 3, 'checked_at', '2026-11-03T15:00:00Z') END,
       (ARRAY['new','new','new','saved','contacted','new'])[1 + n % 6],
       CASE WHEN n % 4 = 3 THEN 'no_website' ELSE 'done' END,
       CASE WHEN n % 4 = 3 THEN NULL ELSE NOW() - INTERVAL '1 day' END,
       CASE WHEN n % 8 = 0 THEN ARRAY['implants'] ELSE '{}'::text[] END,
       '', NOW() - (n || ' minutes')::interval, NOW() - (n || ' minutes')::interval
FROM names;

UPDATE leads SET score = LEAST(100,
    (CASE WHEN website <> '' THEN 15 ELSE 0 END) +
    (CASE WHEN email <> '' THEN 25 ELSE 0 END) +
    (CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(emails) e WHERE e->>'email' = email AND e->>'status' = 'valid') THEN 15 ELSE 0 END) +
    (CASE WHEN phone <> '' THEN 15 ELSE 0 END) +
    (CASE WHEN socials <> '{}'::jsonb THEN 10 ELSE 0 END) +
    (CASE WHEN rating >= 4 THEN 10 WHEN rating >= 3 THEN 5 ELSE 0 END) +
    (CASE WHEN review_count >= 25 THEN 10 WHEN review_count >= 5 THEN 5 ELSE 0 END))
WHERE id::text LIKE '86300000-%';

-- Synthetic people at the businesses with a website, with addresses on each domain's pattern.
WITH roster AS (
  SELECT l.id AS lead_id, right(l.id::text, 12)::int AS n, l.domain, k
  FROM leads l, generate_series(1, 3) AS k
  WHERE l.id::text LIKE '86300000-%' AND l.domain <> '' AND k <= 1 + right(l.id::text, 12)::int % 3
), named AS (
  SELECT lead_id, n, domain, k,
         (ARRAY['Elena','Marcus','Priya','Sofia','Daniel','Grace','Omar','Hannah','Luis','Maya','Ethan','Chloe','Nora','Caleb','Ava','Theo'])[1 + (n * 3 + k * 5) % 16] AS first,
         (ARRAY['Morales','Bennett','Shah','Nguyen','Carter','Kim','Haddad','Fischer','Ortiz','Reyes','Walsh','Park','Lindqvist','Brooks','Okafor','Romano'])[1 + (n * 7 + k * 3) % 16] AS last,
         (ARRAY['Owner & Lead Dentist','Practice Manager','Associate Dentist'])[k] AS title,
         (ARRAY['owner','manager','staff'])[k] AS seniority
  FROM roster
)
INSERT INTO lead_people (organization_id, lead_id, name, name_key, first_name, last_name, title, seniority, email, email_source,
                         email_status, email_confidence, source, source_url)
SELECT '22222222-0000-0000-0000-000000000001', lead_id, first || ' ' || last, lower(first || ' ' || last), first, last, title, seniority,
       CASE WHEN k = 3 AND n % 2 = 1 THEN ''
            ELSE lower(first) || CASE WHEN n % 2 = 0 THEN '.' || lower(last) ELSE '' END || '@' || domain END,
       CASE WHEN k = 3 AND n % 2 = 1 THEN '' WHEN k = 1 AND n % 3 = 1 THEN 'website' ELSE 'pattern' END,
       CASE WHEN k = 3 AND n % 2 = 1 THEN '' WHEN k = 1 AND n % 3 = 1 THEN 'valid' WHEN n % 5 = 1 THEN 'guessed' ELSE 'valid' END,
       CASE WHEN k = 3 AND n % 2 = 1 THEN 0 WHEN k = 1 AND n % 3 = 1 THEN 100 WHEN n % 5 = 1 THEN 60 ELSE 95 END,
       CASE WHEN k = 3 THEN 'schema' ELSE 'page' END,
       'https://' || domain || '/our-team'
FROM named;

UPDATE leads SET site = site || jsonb_build_object('email_pattern', CASE WHEN right(id::text, 12)::int % 2 = 0 THEN 'first.last' ELSE 'first' END)
WHERE id::text LIKE '86300000-%' AND domain <> '';

INSERT INTO lead_search_hits (search_id, lead_id)
SELECT '86300000-0000-0000-0000-000000000000', id FROM leads WHERE id::text LIKE '86300000-%';

-- One website the listing lacked, worked out from the name.
UPDATE leads SET site = site || '{"found_website": true}'::jsonb WHERE id = '86300000-0000-0000-0000-000000000002';

-- A website run part way through: four businesses still being read. The backend leaves a
-- running run alone for 15 minutes, longer than a recording takes.
UPDATE leads SET enrichment_status = 'queued'
WHERE id IN ('86300000-0000-0000-0000-000000000004', '86300000-0000-0000-0000-000000000006',
             '86300000-0000-0000-0000-000000000009', '86300000-0000-0000-0000-000000000010');
INSERT INTO lead_enrichment_runs (id, organization_id, requested_by_user_id, lead_ids, status, total, processed, emails_found, people_found, started_at)
SELECT '86300000-0000-0000-0000-0000000e0001', '22222222-0000-0000-0000-000000000001', '11111111-0000-0000-0000-000000000001',
       array_agg(id ORDER BY id), 'running', 12, 7, 5, 6, NOW()
FROM leads WHERE id::text LIKE '86300000-%' AND right(id::text, 12)::int BETWEEN 1 AND 12;

-- The product tour would cover the page; the flow records the lead finder, not the tour.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;
COMMIT;
