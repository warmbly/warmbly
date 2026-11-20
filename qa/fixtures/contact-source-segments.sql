-- An Apollo connection with one saved import already pinning contacts into a segment, for the source-segments proof.
UPDATE users SET product_tour_completed_at = now()
WHERE id = '11111111-0000-0000-0000-000000000001' AND product_tour_completed_at IS NULL;
INSERT INTO integration_connections (id, organization_id, provider, label, status, auth_method, display_fields, health)
VALUES ('5e650000-0000-4000-8000-000000000001', '22222222-0000-0000-0000-000000000001', 'apollo', 'Apollo', 'connected', 'api_key',
        '{"account_name":"Acme Sales","account_email":"ops@acme.test"}', 'healthy')
ON CONFLICT (id) DO NOTHING;
INSERT INTO segments (id, organization_id, name, color)
VALUES ('5e650000-0000-4000-8000-000000000002', '22222222-0000-0000-0000-000000000001', 'Heads of sales', '#7c3aed')
ON CONFLICT (id) DO NOTHING;
INSERT INTO apollo_import_sources (id, organization_id, connection_id, name, list_id, list_name, recurring, segment_ids, total_imported, last_run_at)
VALUES ('5e650000-0000-4000-8000-000000000003', '22222222-0000-0000-0000-000000000001', '5e650000-0000-4000-8000-000000000001',
        'Heads of sales, EMEA', 'apl-list-emea', 'Heads of sales, EMEA', true, ARRAY['5e650000-0000-4000-8000-000000000002']::uuid[], 184, now() - interval '40 minutes')
ON CONFLICT (id) DO NOTHING;
