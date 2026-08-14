-- Outreach drafts: evidence-bound message generation + human review for
-- CONFENGE staging accounts. Feature-flagged with CONFENGE_OUTREACH_ENABLED.

CREATE TABLE IF NOT EXISTS outreach_drafts (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id         uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    account_id              uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE CASCADE,
    contact_candidate_id    uuid REFERENCES outreach_contact_candidates(id) ON DELETE SET NULL,

    recipient_name          text NOT NULL DEFAULT '',
    recipient_role          text NOT NULL DEFAULT '',
    recipient_email         text NOT NULL DEFAULT '',
    verification_status     text NOT NULL DEFAULT '',

    subject                 text NOT NULL DEFAULT '',
    body_text               text NOT NULL DEFAULT '',
    body_html               text NOT NULL DEFAULT '',
    followups_json          jsonb NOT NULL DEFAULT '[]'::jsonb,

    service_code            text NOT NULL DEFAULT '',
    strategy_code           text NOT NULL DEFAULT '',
    fact_used               text NOT NULL DEFAULT '',
    evidence_ids            jsonb NOT NULL DEFAULT '[]'::jsonb,
    question                text NOT NULL DEFAULT '',
    cta                     text NOT NULL DEFAULT '',

    provider                text NOT NULL DEFAULT '',
    model                   text NOT NULL DEFAULT '',
    prompt_version          text NOT NULL DEFAULT 'confenge.draft.v1',
    generation              int NOT NULL DEFAULT 0,

    validation_json         jsonb NOT NULL DEFAULT '{}'::jsonb,
    risk_class              text NOT NULL DEFAULT 'YELLOW',
    risk_flags              jsonb NOT NULL DEFAULT '[]'::jsonb,
    red_team_result         text NOT NULL DEFAULT '',
    red_team_reasons        jsonb NOT NULL DEFAULT '[]'::jsonb,

    status                  text NOT NULL DEFAULT 'NOT_GENERATED',
    human_edited            boolean NOT NULL DEFAULT false,
    approved_by             uuid REFERENCES users(id) ON DELETE SET NULL,
    approved_at             timestamptz,
    review_seconds          int NOT NULL DEFAULT 0,

    campaign_id             uuid REFERENCES campaigns(id) ON DELETE SET NULL,
    enrollment_contact_id   uuid REFERENCES contacts(id) ON DELETE SET NULL,
    enrolled_at             timestamptz,

    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT outreach_drafts_status_check
        CHECK (status IN (
            'NOT_GENERATED',
            'GENERATING',
            'NEEDS_REVIEW',
            'APPROVED',
            'REJECTED',
            'SKIPPED',
            'BLOCKED',
            'ENROLLED',
            'SENT',
            'REPLIED'
        )),
    CONSTRAINT outreach_drafts_risk_check
        CHECK (risk_class IN ('GREEN', 'YELLOW', 'RED'))
);

CREATE INDEX IF NOT EXISTS outreach_drafts_org_status_idx
    ON outreach_drafts (organization_id, status);

CREATE INDEX IF NOT EXISTS outreach_drafts_account_idx
    ON outreach_drafts (organization_id, account_id);

-- One active review draft per account (not terminal).
CREATE UNIQUE INDEX IF NOT EXISTS outreach_drafts_org_account_active_uidx
    ON outreach_drafts (organization_id, account_id)
    WHERE status IN ('NOT_GENERATED', 'GENERATING', 'NEEDS_REVIEW', 'APPROVED');

-- Org-scoped confenge campaign pointer (bootstrap idempotency).
CREATE TABLE IF NOT EXISTS outreach_org_settings (
    organization_id     uuid PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    campaign_id         uuid REFERENCES campaigns(id) ON DELETE SET NULL,
    campaign_name       text NOT NULL DEFAULT 'CONFENGE | Outreach consultivo inicial',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
