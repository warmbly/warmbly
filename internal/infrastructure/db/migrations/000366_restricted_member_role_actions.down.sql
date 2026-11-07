CREATE OR REPLACE FUNCTION organization_member_access_mask() RETURNS trigger AS $$
BEGIN
    IF NEW.role = 'owner' THEN
        NEW.access_scope := 'workspace';
    END IF;
    IF NEW.access_scope = 'restricted' THEN
        NEW.permissions := NEW.permissions & 1184;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Puts every restricted member back under the read-only mask.
UPDATE organization_members SET permissions = permissions WHERE access_scope = 'restricted' AND role <> 'owner';

ALTER TABLE organization_roles DROP COLUMN IF EXISTS acts_in_scope;
