-- Custom org roles: a named, org-scoped permission set members can be
-- assigned to. Effective permissions stay denormalized on
-- organization_members.permissions (write-through on assign/edit), so every
-- existing reader (Go middleware, realtime auth, API) needs no JOIN; role_id
-- exists to propagate role edits, block deleting in-use roles, and display.
CREATE TABLE organization_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name varchar(50) NOT NULL,
    description text NOT NULL DEFAULT '',
    permissions integer NOT NULL DEFAULT 0,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    UNIQUE (organization_id, name)
);

CREATE INDEX idx_org_roles_org ON organization_roles (organization_id);

ALTER TABLE organization_members
    ADD COLUMN role_id uuid REFERENCES organization_roles(id) ON DELETE SET NULL;

ALTER TABLE organization_invitations
    ADD COLUMN role_id uuid REFERENCES organization_roles(id) ON DELETE SET NULL;

CREATE INDEX idx_org_members_role ON organization_members (role_id) WHERE role_id IS NOT NULL;
