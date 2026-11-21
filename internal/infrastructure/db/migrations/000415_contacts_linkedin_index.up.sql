-- Alone in its file for CONCURRENTLY: contacts is a live table. Finds a workspace's contact by LinkedIn profile.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_contacts_org_linkedin
    ON contacts (organization_id, linkedin_public_id)
    WHERE linkedin_public_id IS NOT NULL;
