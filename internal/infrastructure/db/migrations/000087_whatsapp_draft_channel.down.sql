ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS whatsapp_consent_provenance_ok;
ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS whatsapp_consent_at;
ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS whatsapp_consent_source;
ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS whatsapp_consent_status;
ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS phone_source_url;
ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS phone_source;
ALTER TABLE outreach_contact_candidates DROP COLUMN IF EXISTS phone_e164;

ALTER TABLE outreach_drafts DROP CONSTRAINT IF EXISTS outreach_drafts_channel_check;
DROP INDEX IF EXISTS outreach_drafts_org_channel_idx;
ALTER TABLE outreach_drafts DROP COLUMN IF EXISTS recipient_phone_e164;
ALTER TABLE outreach_drafts DROP COLUMN IF EXISTS channel;
