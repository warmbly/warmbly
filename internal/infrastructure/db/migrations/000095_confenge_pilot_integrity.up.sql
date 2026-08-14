-- Authoritative feed age and durable, capacity-safe pilot membership.

ALTER TABLE outreach_import_runs
    ADD COLUMN IF NOT EXISTS source_generated_at timestamptz;

ALTER TABLE outreach_feed_sync_state
    ADD COLUMN IF NOT EXISTS source_generated_at timestamptz;

COMMENT ON COLUMN outreach_feed_sync_state.last_success_at IS
'Warmbly sync completion time. Never use this as the age of upstream data.';
COMMENT ON COLUMN outreach_feed_sync_state.source_generated_at IS
'Authoritative generated_at from the last completely applied upstream manifest.';

CREATE TABLE IF NOT EXISTS outreach_pilot_operations (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    operation_key   text NOT NULL,
    request_hash    text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, operation_key),
    CONSTRAINT outreach_pilot_operations_key_check CHECK (operation_key <> ''),
    CONSTRAINT outreach_pilot_operations_hash_check CHECK (request_hash ~ '^[a-f0-9]{64}$')
);

CREATE TABLE IF NOT EXISTS outreach_pilot_memberships (
    id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id       uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cohort_id             text NOT NULL,
    account_id            uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE RESTRICT,
    cnpj14                text NOT NULL,
    contact_candidate_id  uuid NOT NULL REFERENCES outreach_contact_candidates(id) ON DELETE RESTRICT,
    touchpoint_id         uuid NOT NULL REFERENCES outreach_touchpoints(id) ON DELETE RESTRICT,
    draft_id              uuid NOT NULL REFERENCES outreach_drafts(id) ON DELETE RESTRICT,
    snapshot_hash         text NOT NULL,
    source_run_id         text NOT NULL,
    context_hash          text NOT NULL,
    operation_key         text NOT NULL DEFAULT '',
    request_hash          text NOT NULL DEFAULT '',
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outreach_pilot_memberships_cohort_check CHECK (cohort_id <> ''),
    CONSTRAINT outreach_pilot_memberships_cnpj_check CHECK (cnpj14 ~ '^[0-9]{14}$'),
    CONSTRAINT outreach_pilot_memberships_snapshot_check CHECK (snapshot_hash <> ''),
    CONSTRAINT outreach_pilot_memberships_source_run_check CHECK (source_run_id <> ''),
    CONSTRAINT outreach_pilot_memberships_context_check CHECK (context_hash <> '')
);

CREATE TABLE IF NOT EXISTS outreach_pilot_slots (
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    cohort_id       text NOT NULL,
    account_id      uuid NOT NULL REFERENCES outreach_accounts(id) ON DELETE RESTRICT,
    cnpj14          text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, cohort_id, account_id),
    CONSTRAINT outreach_pilot_slots_cohort_check CHECK (cohort_id <> ''),
    CONSTRAINT outreach_pilot_slots_cnpj_check CHECK (cnpj14 ~ '^[0-9]{14}$')
);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_pilot_slots_org_cohort_cnpj_uidx
    ON outreach_pilot_slots (organization_id, cohort_id, cnpj14);

CREATE UNIQUE INDEX IF NOT EXISTS outreach_pilot_memberships_org_cohort_account_uidx
    ON outreach_pilot_memberships (organization_id, cohort_id, account_id);
CREATE UNIQUE INDEX IF NOT EXISTS outreach_pilot_memberships_org_cohort_cnpj_uidx
    ON outreach_pilot_memberships (organization_id, cohort_id, cnpj14);
CREATE UNIQUE INDEX IF NOT EXISTS outreach_pilot_memberships_touchpoint_uidx
    ON outreach_pilot_memberships (touchpoint_id);
CREATE UNIQUE INDEX IF NOT EXISTS outreach_pilot_memberships_draft_uidx
    ON outreach_pilot_memberships (draft_id);
CREATE INDEX IF NOT EXISTS outreach_pilot_memberships_org_cohort_idx
    ON outreach_pilot_memberships (organization_id, cohort_id, created_at);
