-- Roles become pure data: every org gets seeded default roles (Admin,
-- Manager, Viewer) that are editable and deletable like any other role, and
-- members reference roles only via role_id. "Owner" stays a special status
-- on the membership row, not a role. Permission values are the defined-bit
-- bundles: Admin = all 15 defined bits minus transfer-ownership (28671),
-- Manager = operational bundle (19964), Viewer = read-only (3104).
ALTER TABLE organization_roles ADD COLUMN color varchar(7) NOT NULL DEFAULT '';

INSERT INTO organization_roles (id, organization_id, name, description, permissions, color)
SELECT gen_random_uuid(), o.id, d.name, d.description, d.permissions, d.color
FROM organizations o
CROSS JOIN (VALUES
    ('Admin', 'Everything except transferring ownership.', 28671, '#8b5cf6'),
    ('Manager', 'Runs campaigns, contacts, mailboxes, and integrations. No team, billing, or settings access.', 19964, '#10b981'),
    ('Viewer', 'Read-only access to campaigns, contacts, and reports.', 3104, '#f59e0b')
) AS d(name, description, permissions, color)
ON CONFLICT (organization_id, name) DO NOTHING;

-- Re-home existing members onto the seeded roles (owner rows stay as-is).
UPDATE organization_members om
SET role_id = r.id, role = r.name, permissions = r.permissions
FROM organization_roles r
WHERE om.role_id IS NULL
  AND om.role <> 'owner'
  AND r.organization_id = om.organization_id
  AND r.name = CASE
        WHEN om.role = 'admin' THEN 'Admin'
        WHEN om.role IN ('manager', 'member') THEN 'Manager'
        ELSE 'Viewer'
      END;
