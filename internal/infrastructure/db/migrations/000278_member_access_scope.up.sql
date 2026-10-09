-- A role says what a member may do; the scope says to which resources ('restricted' is only the grants below).
ALTER TABLE organization_members
    ADD COLUMN access_scope text NOT NULL DEFAULT 'workspace'
        CONSTRAINT organization_members_access_scope_check CHECK (access_scope IN ('workspace', 'restricted'));

-- An invitee lands already restricted; the ids are checked against live resources again on accept.
ALTER TABLE organization_invitations
    ADD COLUMN access_scope text NOT NULL DEFAULT 'workspace'
        CONSTRAINT organization_invitations_access_scope_check CHECK (access_scope IN ('workspace', 'restricted')),
    ADD COLUMN access_folder_ids uuid[] NOT NULL DEFAULT '{}',
    ADD COLUMN access_campaign_ids uuid[] NOT NULL DEFAULT '{}',
    ADD COLUMN access_email_account_ids uuid[] NOT NULL DEFAULT '{}';

-- Mailbox grants: the conversations of these mailboxes.
CREATE TABLE organization_member_mailbox_access (
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    email_account_id uuid NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    granted_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id, email_account_id),
    FOREIGN KEY (organization_id, user_id) REFERENCES organization_members(organization_id, user_id) ON DELETE CASCADE
);
CREATE INDEX idx_member_mailbox_access_account ON organization_member_mailbox_access (email_account_id);

-- Campaign grants: one folder (whatever it holds at read time) or one campaign.
CREATE TABLE organization_member_campaign_access (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    folder_id uuid REFERENCES folders(id) ON DELETE CASCADE,
    campaign_id uuid REFERENCES campaigns(id) ON DELETE CASCADE,
    granted_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT organization_member_campaign_access_target CHECK (num_nonnulls(folder_id, campaign_id) = 1),
    FOREIGN KEY (organization_id, user_id) REFERENCES organization_members(organization_id, user_id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX uniq_member_campaign_access_folder ON organization_member_campaign_access (organization_id, user_id, folder_id) WHERE folder_id IS NOT NULL;
CREATE UNIQUE INDEX uniq_member_campaign_access_campaign ON organization_member_campaign_access (organization_id, user_id, campaign_id) WHERE campaign_id IS NOT NULL;
CREATE INDEX idx_member_campaign_access_folder ON organization_member_campaign_access (folder_id) WHERE folder_id IS NOT NULL;
CREATE INDEX idx_member_campaign_access_campaign ON organization_member_campaign_access (campaign_id) WHERE campaign_id IS NOT NULL;

-- Folder grants resolve through campaign_folders by folder.
CREATE INDEX IF NOT EXISTS idx_campaign_folders_folder ON campaign_folders (folder_id);

-- Every write holds a restricted member to view_campaigns (1024), view_analytics (32) and access_unibox (128); the owner is never restricted.
CREATE FUNCTION organization_member_access_mask() RETURNS trigger AS $$
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

CREATE TRIGGER organization_member_access_mask
    BEFORE INSERT OR UPDATE ON organization_members
    FOR EACH ROW EXECUTE FUNCTION organization_member_access_mask();
