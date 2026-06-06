-- Campaign email attachments. Binary lives in S3 (s3_key); this row is the
-- metadata + ownership record. sequence_id is nullable: NULL = available to
-- every step of the campaign; set = scoped to one step.
CREATE TABLE IF NOT EXISTS campaign_attachments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    sequence_id uuid REFERENCES sequences(id) ON DELETE CASCADE,
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename    text NOT NULL,
    size        bigint NOT NULL,
    mime_type   text NOT NULL,
    s3_key      text NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_campaign_attachments_campaign ON campaign_attachments (campaign_id);
CREATE INDEX IF NOT EXISTS idx_campaign_attachments_sequence ON campaign_attachments (sequence_id);
