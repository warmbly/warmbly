-- On-demand Google Sheets -> leads sync sources.
--
-- A "lead sync source" is a saved, re-runnable binding between a Google Sheet
-- (reached through an existing google_sheets OAuth connection) and Warmbly's
-- contact importer. It is ON-DEMAND only: the user presses "Sync now" and the
-- control plane reads the sheet and upserts contacts by (user, email). There is
-- no background scheduler — nothing here drives the worker.
--
-- column_mapping / category_ids are JSONB so they reuse the exact contact
-- import column-mapping shape ([]ContactImportColumnMapping) and category id
-- list the /contacts/import/commit path already understands.

CREATE TABLE lead_sync_sources (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id     uuid NOT NULL,
    created_by_user_id  uuid NOT NULL,
    provider            text NOT NULL DEFAULT 'google_sheets',
    connection_id       uuid NOT NULL, -- integration_connections row used for Google OAuth
    sheet_id            text NOT NULL,
    sheet_title         text,
    tab_title           text,
    a1_range            text,
    has_header          boolean NOT NULL DEFAULT true,
    column_mapping      jsonb NOT NULL DEFAULT '[]',
    dedup               text NOT NULL DEFAULT 'update'
        CHECK (dedup IN ('skip', 'update', 'create_duplicate')),
    target_campaign_id  uuid,
    category_ids        jsonb NOT NULL DEFAULT '[]',
    subscribed_default  boolean NOT NULL DEFAULT true,
    label               text,
    status              text NOT NULL DEFAULT 'idle',
    last_synced_at      timestamptz,
    last_result         jsonb,
    last_error          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_lead_sync_sources_org ON lead_sync_sources (organization_id);
CREATE INDEX idx_lead_sync_sources_org_campaign ON lead_sync_sources (organization_id, target_campaign_id);
