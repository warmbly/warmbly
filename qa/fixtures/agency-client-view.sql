-- Proof fixture for flows/agency-client-view.flow.ts, applied after fixtures/agency.sql: two roles in the
-- agency's client workspace, so the client view has roles to be drawn for. Re-runnable.
BEGIN;

INSERT INTO organization_roles (organization_id, name, description, permissions, color)
VALUES
    -- View analytics, unified inbox, view campaigns, view contacts.
    ('22222222-0000-0000-0000-000000000001', 'Client team', 'Reads campaigns and answers replies.', 3232, '#10b981'),
    -- The manager template: campaigns, contacts, mailboxes, sending, steps, analytics, inbox, integrations.
    ('22222222-0000-0000-0000-000000000001', 'Client manager', 'Runs campaigns day to day.', 19964, '#8b5cf6')
ON CONFLICT (organization_id, name) DO UPDATE SET permissions = EXCLUDED.permissions, description = EXCLUDED.description;

COMMIT;
