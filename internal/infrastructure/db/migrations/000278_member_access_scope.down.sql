-- Rolling back would make every restricted member workspace-wide.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM organization_members WHERE access_scope = 'restricted')
        OR EXISTS (SELECT 1 FROM organization_invitations WHERE access_scope = 'restricted') THEN
        RAISE EXCEPTION 'restricted members or invitations exist; set them to the entire workspace or remove them first';
    END IF;
END $$;

DROP TRIGGER IF EXISTS organization_member_access_mask ON organization_members;
DROP FUNCTION IF EXISTS organization_member_access_mask();
DROP INDEX IF EXISTS idx_campaign_folders_folder;
DROP TABLE IF EXISTS organization_member_campaign_access;
DROP TABLE IF EXISTS organization_member_mailbox_access;
ALTER TABLE organization_invitations
    DROP COLUMN access_email_account_ids,
    DROP COLUMN access_campaign_ids,
    DROP COLUMN access_folder_ids,
    DROP COLUMN access_scope;
ALTER TABLE organization_members DROP COLUMN access_scope;
