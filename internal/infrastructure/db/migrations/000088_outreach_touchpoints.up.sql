-- CONFENGE per-touchpoint human approval state machine.
CREATE TABLE IF NOT EXISTS outreach_touchpoints (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id              uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE CASCADE,
    contact_candidate_id    uuid REFERENCES outreach_contact_candidates(id) ON DELETE SET NULL,
    ordinal                 int NOT NULL DEFAULT 1,
    cadence_step            text NOT NULL DEFAULT '',
    channel                 text NOT NULL DEFAULT 'EMAIL',
    purpose                 text NOT NULL DEFAULT 'INITIAL',
    due_at                  timestamptz NOT NULL DEFAULT now(),
    state                   text NOT NULL DEFAULT 'PLANNED',
    draft_id                uuid REFERENCES outreach_drafts(id) ON DELETE SET NULL,
    recipient               text NOT NULL DEFAULT '',
    subject                 text NOT NULL DEFAULT '',
    body_text               text NOT NULL DEFAULT '',
    content_hash            text NOT NULL DEFAULT '',
    approved_content_hash   text NOT NULL DEFAULT '',
    approved_by             uuid REFERENCES users(id) ON DELETE SET NULL,
    approved_at             timestamptz,
    queued_at               timestamptz,
    sent_at                 timestamptz,
    provider_message_id     text NOT NULL DEFAULT '',
    stop_reason             text NOT NULL DEFAULT '',
    previous_touchpoint_id  uuid REFERENCES outreach_touchpoints(id) ON DELETE SET NULL,
    idempotency_key         text NOT NULL DEFAULT '',
    policy_version          text NOT NULL DEFAULT 'confenge.cadence.v1',
    service_code            text NOT NULL DEFAULT '',
    fact_used               text NOT NULL DEFAULT '',
    evidence_ids            jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outreach_touchpoints_channel_check CHECK (channel IN ('EMAIL', 'WHATSAPP')),
    CONSTRAINT outreach_touchpoints_state_check CHECK (state IN (
        'PLANNED','DUE','DRAFTED','NEEDS_REVIEW','APPROVED','QUEUED','SENT',
        'SKIPPED','REJECTED','REPLIED','DNC','BOUNCED','CANCELLED','FAILED')),
    CONSTRAINT outreach_touchpoints_ordinal_check CHECK (ordinal >= 1 AND ordinal <= 20)
);
CREATE UNIQUE INDEX IF NOT EXISTS outreach_touchpoints_org_account_ordinal_open_uidx
    ON outreach_touchpoints (organization_id, account_id, ordinal)
    WHERE state NOT IN ('SKIPPED','REJECTED','REPLIED','DNC','BOUNCED','CANCELLED','FAILED','SENT');
CREATE UNIQUE INDEX IF NOT EXISTS outreach_touchpoints_org_idempotency_uidx
    ON outreach_touchpoints (organization_id, idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS outreach_touchpoints_org_state_due_idx ON outreach_touchpoints (organization_id, state, due_at);
CREATE INDEX IF NOT EXISTS outreach_touchpoints_org_account_idx ON outreach_touchpoints (organization_id, account_id, ordinal);
CREATE INDEX IF NOT EXISTS outreach_touchpoints_org_review_idx ON outreach_touchpoints (organization_id, state)
    WHERE state IN ('DUE','DRAFTED','NEEDS_REVIEW','APPROVED');
