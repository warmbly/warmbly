-- A role marked acts_in_scope lets a restricted member act with it inside their grants; any other role gives them only reads.
ALTER TABLE organization_roles
    ADD COLUMN IF NOT EXISTS acts_in_scope boolean NOT NULL DEFAULT false;

-- A restricted member keeps view_campaigns (1024), view_analytics (32) and access_unibox (128) from any role, and
-- manage_campaigns (4) and send_campaigns (64) only from a role that acts in scope; the owner is never restricted.
CREATE OR REPLACE FUNCTION organization_member_access_mask() RETURNS trigger AS $$
BEGIN
    IF NEW.role = 'owner' THEN
        NEW.access_scope := 'workspace';
    END IF;
    IF NEW.access_scope = 'restricted' THEN
        NEW.permissions := NEW.permissions & (1184 | (1252 & COALESCE((
            SELECT bit_or(r.permissions)::int FROM organization_member_roles mr
            JOIN organization_roles r ON r.id = mr.role_id
            WHERE mr.organization_id = NEW.organization_id AND mr.user_id = NEW.user_id AND r.acts_in_scope), 0)));
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
