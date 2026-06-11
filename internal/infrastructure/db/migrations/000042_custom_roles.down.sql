ALTER TABLE organization_invitations DROP COLUMN IF EXISTS role_id;
ALTER TABLE organization_members DROP COLUMN IF EXISTS role_id;
DROP TABLE IF EXISTS organization_roles;
