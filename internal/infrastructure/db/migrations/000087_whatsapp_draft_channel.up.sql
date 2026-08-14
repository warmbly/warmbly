-- Channel discriminator for outreach drafts (EMAIL default, WHATSAPP optional).
-- Additive and backward compatible.

ALTER TABLE outreach_drafts
    ADD COLUMN IF NOT EXISTS channel text NOT NULL DEFAULT 'EMAIL';

ALTER TABLE outreach_drafts
    ADD COLUMN IF NOT EXISTS recipient_phone_e164 text NOT NULL DEFAULT '';

ALTER TABLE outreach_drafts
    DROP CONSTRAINT IF EXISTS outreach_drafts_channel_check;

ALTER TABLE outreach_drafts
    ADD CONSTRAINT outreach_drafts_channel_check
    CHECK (channel IN ('EMAIL', 'WHATSAPP'));

CREATE INDEX IF NOT EXISTS outreach_drafts_org_channel_idx
    ON outreach_drafts (organization_id, channel, status);

-- Optional structured phone provenance on contact candidates (legacy phone text remains).
ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS phone_e164 text NOT NULL DEFAULT '';

ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS phone_source text NOT NULL DEFAULT '';

ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS phone_source_url text NOT NULL DEFAULT '';

ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS whatsapp_consent_status text NOT NULL DEFAULT 'UNKNOWN';

ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS whatsapp_consent_source text NOT NULL DEFAULT '';

ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS whatsapp_consent_at timestamptz;

ALTER TABLE outreach_contact_candidates
    ADD COLUMN IF NOT EXISTS whatsapp_consent_provenance_ok boolean NOT NULL DEFAULT false;
