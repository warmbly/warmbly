BEGIN;

ALTER TABLE pool_link_codes ADD COLUMN remote_organization_id uuid;
ALTER TABLE pool_link_instances ADD COLUMN remote_organization_id uuid;

ALTER TABLE cloud_link DROP CONSTRAINT cloud_link_pkey;
ALTER TABLE cloud_link DROP COLUMN id;
ALTER TABLE cloud_link ADD PRIMARY KEY (instance_id);
ALTER TABLE cloud_link ADD COLUMN organization_id uuid REFERENCES organizations(id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX cloud_link_organization ON cloud_link (organization_id) WHERE organization_id IS NOT NULL;
CREATE UNIQUE INDEX cloud_link_legacy ON cloud_link ((organization_id IS NULL)) WHERE organization_id IS NULL;

ALTER TABLE cloud_link_mailboxes ADD COLUMN instance_id uuid;
UPDATE cloud_link_mailboxes SET instance_id = (SELECT instance_id FROM cloud_link);
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM cloud_link_mailboxes WHERE instance_id IS NULL) THEN
        RAISE EXCEPTION 'Cloud enrollments have no instance link. Restore the link or remove orphan enrollments before upgrading';
    END IF;
END $$;
ALTER TABLE cloud_link_mailboxes ALTER COLUMN instance_id SET NOT NULL;
ALTER TABLE cloud_link_mailboxes ADD CONSTRAINT cloud_link_mailboxes_link_fk FOREIGN KEY (instance_id) REFERENCES cloud_link(instance_id) ON DELETE CASCADE;
CREATE INDEX cloud_link_mailboxes_instance ON cloud_link_mailboxes (instance_id);

UPDATE placement_tests SET remote_instance_id = (SELECT instance_id FROM cloud_link WHERE organization_id IS NULL)
WHERE remote_test_id IS NOT NULL AND remote_instance_id IS NULL;

ALTER TABLE domain_redirects ADD COLUMN cloud_link_instance_id uuid;
UPDATE domain_redirects SET cloud_link_instance_id = (SELECT instance_id FROM cloud_link WHERE organization_id IS NULL)
WHERE served_by = 'cloud' AND linked_instance_id IS NULL;

COMMIT;
