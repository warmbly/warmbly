BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM cloud_link WHERE organization_id IS NOT NULL)
       OR EXISTS (SELECT 1 FROM pool_link_instances WHERE remote_organization_id IS NOT NULL AND revoked_at IS NULL) THEN
        RAISE EXCEPTION 'Disconnect workspace-scoped Cloud links before downgrading';
    END IF;
END $$;
ALTER TABLE domain_redirects DROP COLUMN cloud_link_instance_id;
UPDATE placement_tests SET remote_instance_id = NULL WHERE remote_test_id IS NOT NULL;
ALTER TABLE cloud_link_mailboxes DROP COLUMN instance_id;
DROP INDEX cloud_link_organization;
DROP INDEX cloud_link_legacy;
ALTER TABLE cloud_link DROP COLUMN organization_id;
ALTER TABLE cloud_link DROP CONSTRAINT cloud_link_pkey;
ALTER TABLE cloud_link ADD COLUMN id boolean PRIMARY KEY DEFAULT true CHECK (id);
ALTER TABLE pool_link_instances DROP COLUMN remote_organization_id;
ALTER TABLE pool_link_codes DROP COLUMN remote_organization_id;

COMMIT;
